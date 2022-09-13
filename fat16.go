package lipid

import (
	"errors"
	"io/ioutil"
	"os"
	"strings"
)

const VOLUME_START int64 = 0x0

//name of formatting os, 8 bytes long
const OEM_ID string = "lipid.go"

//0x0000         free cluster
//0x0001, 0x0002 illegal
//0x0003-0xffef  number of next cluster
//0xfff7         1+ bad sectors
//0xfff8-0xffff  end of file

//for testing compadibility
var DirectoryEntryOffsets = directoryEntryOffsetsStruct{
	offsetObject{0x00, 8}, //Filename
	offsetObject{0x08, 3}, //FilenameExtension
	offsetObject{0x0B, 1}, //AttributeByte
	offsetObject{0x0C, 1}, //Reserved
	offsetObject{0x0D, 1}, //Creation
	offsetObject{0x0E, 2}, //CreationTime
	offsetObject{0x10, 2}, //CreationDate
	offsetObject{0x12, 2}, //LastAccessDate
	offsetObject{0x14, 2}, //ReservedFat32
	offsetObject{0x16, 2}, //LastWriteTime
	offsetObject{0x18, 2}, //LastWriteDate
	offsetObject{0x1A, 2}, //StartingCluster
	offsetObject{0x1C, 4}, //FileSize
}

type Fat16 struct {
	*fat16
}

type fat16 struct {
	File             *os.File
	RegionOffsets    fileSystemOffsetStruct
	CurrentDirOffset int64
	CommonSizes      sizesStruct
}

//open a fat16 image
func OpenFat16Image(path string) (*Fat16, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0755)
	if err != nil {
		return &Fat16{nil}, err
	}

	commonSizes := sizesStruct{
		SectorsPerCluster: getValue(file, BootSector.SectorsPerCluster),
		BytesPerSector:    getValue(file, BootSector.BytesPerSector),
		SectorsPerFat:     getValue(file, BootSector.SectorsPerFat),
		BytesPerCluster:   getValue(file, BootSector.BytesPerSector) * getValue(file, BootSector.SectorsPerCluster),
	}

	hexData := getRegionData(file)
	f := &Fat16{&fat16{
		File:             file,
		RegionOffsets:    hexData,
		CurrentDirOffset: hexData.RootDirRegion.Offset,
		CommonSizes:      commonSizes,
	}}

	return f, nil
}
func (f *fat16) Close() { f.File.Close() }

//get offset of provided cluster number
func (f *fat16) GetClusterOffset(clusterN int64) int64 {
	bytesPerSector := getValue(f.File, BootSector.BytesPerSector)
	sectorsPerCluster := getValue(f.File, BootSector.SectorsPerCluster)

	firstSector := (f.RegionOffsets.DataRegion.Offset) + ((clusterN - 2) * sectorsPerCluster * bytesPerSector)

	return firstSector
}

//get FAT sector of provided cluster
func (f *fat16) GetClusterSector(fatSectorN int64) int64 {
	bytesPerSector := getValue(f.File, BootSector.BytesPerSector)
	fatNumOffset := f.RegionOffsets.FATRegion.Offset
	clusterSector := fatNumOffset + (fatSectorN*2)/bytesPerSector

	return clusterSector
}

//read a file from a given offset
func (f *fat16) ReadFile(path string, outPath string) error {
	fileOffset := f.findOffset(f.CurrentDirOffset, path)
	if fileOffset == -1 {
		errorMessage := "file " + path + " not found"
		return errors.New(errorMessage)
	}

	fileSize := getValue(f.File, offsetObject{directoryEntryOffsets.FileSize.Offset + fileOffset, directoryEntryOffsets.FileSize.Length})
	clusterSize := getValue(f.File, BootSector.BytesPerSector) * getValue(f.File, BootSector.SectorsPerCluster)

	fileDirEntry := offsetObject{directoryEntryOffsets.StartingCluster.Offset + fileOffset, directoryEntryOffsets.StartingCluster.Length}
	startCluster := getValue(f.File, fileDirEntry)

	fatNumOffset := (f.RegionOffsets.FATRegion.Offset)

	//generate cluster chain
	fileClusterChain := []int64{startCluster}
	nextChainLink := startCluster
	for {
		//get chain link
		nextChainLink = getValue(f.File, offsetObject{fatNumOffset + nextChainLink*2, 2})
		//check for end of chain
		if nextChainLink == 0xFFFF {
			break
		}
		fileClusterChain = append(fileClusterChain, nextChainLink)
	}

	//name := f.readName(fileOffset)

	err := ioutil.WriteFile(outPath, []byte(""), 0755)
	if err != nil {
		return err
	}
	outFile, err := os.OpenFile(outPath, os.O_APPEND|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	defer outFile.Close()

	//read data from filesystem
	for i, cluster := range fileClusterChain {
		//determine how many bytes we need to read
		passedBytes := int64(i) * clusterSize
		numberOfBytes := fileSize - passedBytes

		if numberOfBytes > clusterSize {
			numberOfBytes = clusterSize
		}

		//write cluster to output file
		clusterOffset := (f.RegionOffsets.DataRegion.Offset) + ((cluster - 2) * clusterSize)
		byteArray, err := readBytes(f.File, clusterOffset, numberOfBytes, false)
		if err != nil {
			return err
		}
		outFile.Write(byteArray)
	}

	return nil
}

//change dir, use '/' as seperator
func (f *fat16) ChangeDir(newPath string) error {
	offset, err := f.getPathOffset(newPath)
	if err != nil {
		return err
	}

	f.CurrentDirOffset = offset
	return nil
}

//list contents of current working directory
func (f *fat16) ListCurrentDir() ([]string, error) {
	return f.ListDir(".")
}

//list contents of directory at provided path
func (f *fat16) ListDir(path string) ([]string, error) {

	returnSlice := make([]string, 0)
	var dirOffset int64
	if path == "." {
		dirOffset = f.CurrentDirOffset
	} else {
		var err error
		dirOffset, err = f.getPathOffset(path)
		if err != nil {
			return make([]string, 0), errors.New("could not find " + path)
		}
	}
	if dirOffset == f.RegionOffsets.RootDirRegion.Offset || getValue(f.File, offsetObject{dirOffset + 0x1A, 2}) == 0x00 {
		i := int64(0x00)
		//set dirOffset to root offset (if right side of OR is true, this may not have the correct value)
		dirOffset = f.RegionOffsets.RootDirRegion.Offset
		for i < f.RegionOffsets.RootDirRegion.Length {
			off := dirOffset + i
			lfnOff := int64(1)
			//check for LFN
			if getValue(f.File, offsetObject{off + 0xB, 1}) == 0x0F {
				lfnOff += int64(getValue(f.File, offsetObject{off, 1}) & 0x3F)
			}
			name := f.readName(off)
			if name != "" {
				returnSlice = append(returnSlice, name)
			}
			i += (lfnOff * 32) //one file metadata unit is 32 bytes
		}
		return returnSlice, nil
	}

	//check if "Dir" is actually a directory
	attributeByte := getValue(f.File, offsetObject{directoryEntryOffsets.AttributeByte.Offset + dirOffset, directoryEntryOffsets.AttributeByte.Length})
	if (attributeByte & 0x10) == 0x10 {
		clusterNumberOffset := int64(0x1A + dirOffset)
		clusterN := getValue(f.File, offsetObject{clusterNumberOffset, 2})
		sectorsPerCluster := getValue(f.File, BootSector.SectorsPerCluster)
		bytesPerSector := getValue(f.File, BootSector.BytesPerSector)
		clusterSize := f.CommonSizes.BytesPerCluster

		clusterOffset := (f.RegionOffsets.DataRegion.Offset) + ((clusterN - 2) * sectorsPerCluster * bytesPerSector)

		i := int64(0x00)
		for i < clusterSize {
			off := clusterOffset + i
			lfnOff := int64(1)
			//check for LFN
			if getValue(f.File, offsetObject{off + 0xB, 1}) == 0x0F {
				lfnOff += int64(getValue(f.File, offsetObject{off, 1}) & 0x3F)
			}
			name := f.readName(off)
			if name != "" {
				returnSlice = append(returnSlice, name)
			}
			i += (lfnOff * 32) //one file metadata unit is 32 bytes
		}
		return returnSlice, nil
	}
	return make([]string, 0), errors.New(path + " is not a directory")
}

//make a directory
func (f *fat16) MakeDir(name string) error {
	//make entry
	entryOff, err := f.makeEntry(name)
	if err != nil {
		return err
	}
	//set bit 0x10 on attribute byte
	attrByte := byte(getValue(f.File, offsetObject{entryOff + 0x0B, 1})) | 0x10
	err = writeBytes(f.File, []byte{attrByte}, entryOff+0x0B)
	if err != nil {
		return err
	}

	//clear out folder cluster
	childCluster := getValue(f.File, offsetObject{entryOff + 0x1A, 2})
	f.clearCluster(childCluster)

	//CREATE . AND .. ENTRIES
	//create '.' entry array
	temp := f.GetClusterOffset(childCluster)

	childDirByteArray := make([]byte, 0)
	childDirByteArray = append(childDirByteArray, []byte(".")...)
	for len(childDirByteArray) < 8 {
		childDirByteArray = append(childDirByteArray, 0x20)
	}
	childDirByteArray = append(childDirByteArray, []byte("")...)
	for len(childDirByteArray) < 11 {
		childDirByteArray = append(childDirByteArray, 0x20)
	}
	//attribute byte
	childDirByteArray = append(childDirByteArray, 0x10)
	//attribute, reserved, and creation
	for i := 0; i < 4; i++ {
		childDirByteArray = append(childDirByteArray, 0x00)
	}
	//creation date
	childDirByteArray = append(childDirByteArray, []byte{0x00, 0x21}...)
	//last access date
	childDirByteArray = append(childDirByteArray, []byte{0x00, 0x21}...)
	//reserved, last write time
	for i := 0; i < 4; i++ {
		childDirByteArray = append(childDirByteArray, 0x00)
	}
	//last write date
	childDirByteArray = append(childDirByteArray, []byte{0x00, 0x21}...)
	//starting cluster
	childDirByteArray = append(childDirByteArray, byte((childCluster)&0x0000FFFF))
	childDirByteArray = append(childDirByteArray, byte((childCluster)&0xFFFF0000))
	//file size
	for i := 0; i < 4; i++ {
		childDirByteArray = append(childDirByteArray, 0x00)
	}

	//create '..' entry array
	parentCluster := f.GetClusterOffset(temp)
	parentDirByteArray := make([]byte, 0)
	parentDirByteArray = append(parentDirByteArray, []byte("..")...)
	for len(parentDirByteArray) < 8 {
		parentDirByteArray = append(parentDirByteArray, 0x20)
	}
	parentDirByteArray = append(parentDirByteArray, []byte("")...)
	for len(parentDirByteArray) < 11 {
		parentDirByteArray = append(parentDirByteArray, 0x20)
	}
	//attribute byte
	parentDirByteArray = append(parentDirByteArray, 0x10)
	//reserved, and creation
	for i := 0; i < 4; i++ {
		parentDirByteArray = append(parentDirByteArray, 0x00)
	}
	//creation date
	parentDirByteArray = append(parentDirByteArray, []byte{0x00, 0x21}...)
	//last access date
	parentDirByteArray = append(parentDirByteArray, []byte{0x00, 0x21}...)
	//reserved, last write time
	for i := 0; i < 4; i++ {
		parentDirByteArray = append(parentDirByteArray, 0x00)
	}
	//last write date
	parentDirByteArray = append(parentDirByteArray, []byte{0x00, 0x21}...)
	//starting cluster
	parentDirByteArray = append(parentDirByteArray, byte((parentCluster)&0x0000FFFF))
	parentDirByteArray = append(parentDirByteArray, byte((parentCluster)&0xFFFF0000))
	//file size
	for i := 0; i < 4; i++ {
		parentDirByteArray = append(parentDirByteArray, 0x00)
	}

	err = writeBytes(f.File, childDirByteArray, temp)
	if err != nil {
		return err
	}
	err = writeBytes(f.File, parentDirByteArray, temp+32)
	if err != nil {
		return err
	}

	return nil
}

//remove an entry
func (f *fat16) Remove(name string) error {
	//get offset of entry to remove
	offset, err := f.getPathOffset(name)
	if err != nil {
		return err
	}

	//entry is a directory
	if (getValue(f.File, offsetObject{offset + 0x0B, 1})&0x10 == 0x10) {
		//entry is not a special directory, remove files and proceed
		if (f.readName(offset) != ".") && (f.readName(offset) != "..") {
			subEntries, _ := f.ListDir(name)
			for _, s := range subEntries {
				err := f.Remove((name + "/" + s))
				if err != nil {
					return err
				}
			}
		} else {
			//special directory (. or ..), mark entry as removed and return
			err = writeBytes(f.File, []byte{0xE5}, offset)
			if err != nil {
				return err
			}
			return nil
		}
	}

	//mark non-lfn entry as removed
	err = writeBytes(f.File, []byte{0xE5}, offset)
	if err != nil {
		return err
	}
	//mark lfn entry(s) as removed
	for i := int64(1); getValue(f.File, offsetObject{(offset - (i * 32) + 0x0B), 1}) == 0x0F && getValue(f.File, offsetObject{(offset - (i * 32)), 1}) != 0xE5; i++ {
		err = writeBytes(f.File, []byte{0xE5}, offset-(i*32))
		if err != nil {
			return err
		}
	}

	//free FAT data
	numberOfFats := getValue(f.File, BootSector.FatCopies)
	for fatNum := 0; fatNum < int(numberOfFats); fatNum++ {
		fatNumOffset := f.RegionOffsets.FATRegion.Offset + (int64(fatNum) * (f.RegionOffsets.FATRegion.Length / numberOfFats))
		nextClusterNumber := getValue(f.File, offsetObject{offset + 0x1A, 2})
		for nextClusterNumber != 0xFFFF {
			temp := nextClusterNumber
			nextClusterNumber = getValue(f.File, offsetObject{fatNumOffset + nextClusterNumber*2, 2})
			writeBytes(f.File, []byte{0x00, 0x00}, fatNumOffset+(2*temp))
		}
	}

	return nil
}

//create an empty file at a given path
func (f *fat16) MakeEmptyFile(path string) (int64, error) {
	return f.makeEntry(path)
}

//add a file to the FAT image
func (f *fat16) AddFile(inFilePath string, imgPath string) error {
	//open file to add to FAT image
	inFile, err := os.Open(inFilePath)
	if err != nil {
		return err
	}
	defer inFile.Close()

	//determine how many clusters the file needs
	inFileStats, err := inFile.Stat()
	if err != nil {
		return err
	}
	fileSize := inFileStats.Size()

	//verify file is smaller than 4GB
	if fileSize > 0xFFFFFFFF {
		return errors.New(inFilePath + " is larger than 4GB, which is unsupported by FAT")
	}

	numberOfClusters := fileSize / f.CommonSizes.BytesPerCluster
	if fileSize%f.CommonSizes.BytesPerCluster != 0 || numberOfClusters == 0 {
		numberOfClusters++
	}

	//calculate FAT chain
	fatChain := make([]int16, 0)
	fatOffset := f.RegionOffsets.FATRegion.Offset
	latestFatOffset := int64(4)
	for i := int64(0); i < numberOfClusters; i++ {
		for latestFatOffset += 2; getValue(f.File, offsetObject{fatOffset + latestFatOffset, 2}) != 0; latestFatOffset += 2 {
			if latestFatOffset > f.RegionOffsets.FATRegion.Length {
				return errors.New("not enough free space to add file " + inFilePath)
			}
		}
		fatChain = append(fatChain, int16(latestFatOffset/2))
	}

	//add entry
	entryOffset, err := f.makeEntry(imgPath)
	if err != nil {
		return err
	}

	//update file size in entry
	sizeBytes := []byte{byte(fileSize & 0x000000FF), byte((fileSize & 0x0000FF00) >> 8), byte((fileSize & 0x00FF0000) >> 16), byte((fileSize & 0xFF000000) >> 24)}
	err = writeBytes(f.File, sizeBytes, entryOffset+0x1C)
	if err != nil {
		return err
	}

	for s, i := range fatChain {
		seekPos := int64(s) * f.CommonSizes.BytesPerCluster
		clusterOffset := f.GetClusterOffset(int64(i))
		err := f.clearCluster(int64(i))
		if err != nil {
			return err
		}

		//create byte slice to fill one sector, repeat until cluster is filled
		for j := int64(0); j < f.CommonSizes.SectorsPerCluster && (seekPos+(j*f.CommonSizes.BytesPerSector)) < inFileStats.Size(); j++ {
			//read bytes from inFile
			sectorBytes, err := readBytes(inFile, seekPos+(j*f.CommonSizes.BytesPerSector), f.CommonSizes.BytesPerSector, false)
			if err != nil {
				return err
			}

			//write bytes to fat image
			writeBytes(f.File, sectorBytes, clusterOffset+(j*f.CommonSizes.BytesPerSector))
		}
	}

	//write FAT chain
	numFats := getValue(f.File, offsetObject{BootSector.FatCopies.Offset, BootSector.FatCopies.Length})
	for i := 0; i < int(numFats); i++ {
		fatOffset = f.RegionOffsets.FATRegion.Offset + int64(i)*f.RegionOffsets.FATRegion.Length

		//update FAT
		for j := 1; j < len(fatChain); j++ {
			currentClusterNumber := fatChain[j-1]
			nextClusterNumber := fatChain[j]
			clusterFatOffset := fatOffset + (int64(currentClusterNumber) * 2)

			clusterBytes := []byte{byte((nextClusterNumber) & 0x00FF), byte((int(nextClusterNumber) & 0xFF00) >> 8)}
			writeBytes(f.File, clusterBytes, clusterFatOffset)
		}
		//set last entry in fatChain to have 0xFFFF
		clusterBytes := []byte{0xFF, 0xFF}
		clusterFatOffset := fatOffset + (int64(fatChain[len(fatChain)-1]) * 2)
		writeBytes(f.File, clusterBytes, clusterFatOffset)
	}

	return nil
}

//move an entry
func (f *fat16) Move(inPath string, outPath string) error {
	inOffset, err := f.getPathOffset(inPath)
	if err != nil {
		return err
	}
	inEntryIsDir := (getValue(f.File, offsetObject{inOffset + 0x0B, 1}) & 0x10) == 0x10

	//create partial outpath (used if for a rename)
	outSplit := strings.Split(outPath, "/")
	outPathPartial := ""
	dirMode := false
	//remove '/' ("") from end if necessary
	//if outPath[len(outPath)-1] == '/' {
	if outSplit[len(outSplit)-1] == "" {
		outSplit = outSplit[:len(outSplit)-1]
		dirMode = true
	}
	for i := 0; i < len(outSplit)-1; i++ {
		if outSplit[i] == "" {
			outPathPartial += "/"
		} else {
			outPathPartial += outSplit[i]
		}
	}

	//check for regular move
	renameMode := false
	outOffset, err := f.getPathOffset(outPath)
	if err != nil {
		//not a regular move, check for rename move
		outOffset, err = f.getPathOffset(outPathPartial)
		if err != nil {
			return err
		} else {
			renameMode = true
		}
	}

	//check for directory rename contradiction
	if renameMode && dirMode && !inEntryIsDir {
		return errors.New("cannot move regular file " + inPath + " to " + outPath + ": not a directory")
	}

	if !renameMode {
		var entries = int64(0)
		//count lfn entries (if present)
		if getValue(f.File, offsetObject{inOffset - 32 + 0x0B, 1}) == 0x0F {
			//count number of lfn entries
			for entries = 1; (getValue(f.File, offsetObject{inOffset - (32 * entries), 1}) & 0x40) != 0x40; entries++ {
			}
		}
		//add an entry for non-lfn name
		entries++

		//read entry bytes
		bytesToWrite, err := readBytes(f.File, inOffset-(32*(entries-1)), (entries * 32), false)
		if err != nil {
			return err
		}

		//locate region to write entry to
		//get offset of directory cluster
		outClusterOffset := getValue(f.File, offsetObject{outOffset + 0x1A, 2})
		if outClusterOffset == -1 {
			return errors.New("an unexpected error has occurred")
		}
		dirClusterOffset := f.GetClusterOffset(outClusterOffset)
		if dirClusterOffset == -1 {
			return errors.New("an unexpected error has occurred")
		}
		outDirOffset := dirClusterOffset

		//determine size of search region
		var regionSize int64
		if outDirOffset == f.RegionOffsets.RootDirRegion.Offset {
			regionSize = f.RegionOffsets.RootDirRegion.Length
		} else {
			regionSize = f.CommonSizes.BytesPerCluster
		}

		//look through cluster for location of entry
		entryOffset := int64(-1)
		for i := 0; i < int(regionSize); i += 32 {
			found := true
			off := outDirOffset + int64(i)
			//look for section with appropriate number of adjacent entries
			for j := int64(0); j < entries; j++ {
				//check if entry value is free
				val := getValue(f.File, offsetObject{off + int64(j*32), 1})
				if !(val == 0x00 || val == 0xE5) {
					found = false
					break
				}
			}
			//entry is found
			if found {
				entryOffset = off
				break
			}
		}
		//no space found
		if entryOffset == -1 {
			return errors.New("no space left in cluster")
		}

		//adjust non-lfn entry as needed
		bytesToWriteLength := len(bytesToWrite)
		regularName := ""
		regularExt := ""
		for i := 0; i < 8; i++ {
			regularName += string(bytesToWrite[bytesToWriteLength-32+i])
		}
		for i := 0; i < 3; i++ {
			regularExt += string(bytesToWrite[bytesToWriteLength-24+i])
		}

		temp := []byte(regularName)
		tempExt := ""
		if regularExt != "" {
			tempExt = "." + regularExt
		}
		for f.findOffset(outDirOffset, (string(temp)+tempExt)) != -1 {
			//increase last character
			temp[7] += 1
			//check for rollover
			if temp[7] == 0x3A {
				temp[7] = '0'
				temp[5] += 1
			}
			//check for rollover in all other characters
			for i := 5; i > 0; i-- {
				if temp[i] == 0x5B {
					temp[i] = 'A'
					temp[i-1] += 1
				}
			}
			//check final character for rollover (entry cannot be created)
			if temp[0] == 0x5B {
				return errors.New("could not create entry with this name")
			}
		}
		//write adjusted name back to bytesToWrite
		for i, c := range temp {
			bytesToWrite[bytesToWriteLength-32+i] = c
		}

		//write entry to new location
		err = writeBytes(f.File, bytesToWrite, entryOffset)
		if err != nil {
			return err
		}

		//mark original entry as removed
		for i := int64(0); i < entries; i++ {
			writeBytes(f.File, []byte{0xE5}, inOffset-(32*i))
		}

		//if entry moved was a directory, update .. subentry
		if inEntryIsDir {
			movedClusterOffset := int64(bytesToWrite[bytesToWriteLength-32+0x1A]) + (int64(bytesToWrite[bytesToWriteLength-32+0x1B]) << 8)
			movedDirClusterOffset := f.GetClusterOffset(movedClusterOffset)
			if dirClusterOffset == -1 {
				return errors.New("an unexpected error has occurred")
			}
			entryOffset := f.findOffset(movedDirClusterOffset, "..")
			bytesToWrite := []byte{byte(outClusterOffset & 0x00FF), byte((outClusterOffset & 0xFF00) >> 8)}

			//write new offset to entry
			writeBytes(f.File, bytesToWrite, entryOffset)
		}

	} else {
		return errors.New("Move operation renames file, not implemented yet")
	}

	return nil
}

/*
//create a fat16 image
func MakeFat16(imgPath string, imgSizeBytes int64, args fatArgs) (*fat16, error) {
	//open file
	file, err := os.Create(imgPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	//adjust file size
	err = file.Truncate(imgSizeBytes)
	if err != nil {
		return nil, err
	}

	//create FAT header
	jumpBytes := []byte{0xEB, 0x3C, 0x90}
	oemBytes := []byte(OEM_ID)

	//convert args to byte array
	if !(args.BytesPerSector == 512 || args.BytesPerSector == 1024 || args.BytesPerSector == 2048 || args.BytesPerSector == 4096) {
		return nil, errors.New(strconv.FormatInt(int64(args.BytesPerSector), 10) + " is not a valid value for BytesPerSector! (512, 1024, 2048, or 4096)")
	}
	bytesPerSector := []byte{byte(args.BytesPerSector & 0x00FF), byte((int(args.BytesPerSector) & 0xFF00) >> 8)}

	if !(args.SectorsPerCluster == 1 || args.SectorsPerCluster == 2 || args.SectorsPerCluster == 4 || args.SectorsPerCluster == 8 || args.SectorsPerCluster == 16 || args.SectorsPerCluster == 32 || args.SectorsPerCluster == 64 || args.SectorsPerCluster == 128) && args.SectorsPerCluster != 255 {
		return nil, errors.New(strconv.FormatInt(int64(args.SectorsPerCluster), 10) + " is not a valid value for SectorsPerCluster! (1, 2, 4, 8, 16, 32, 64, or 128, or 255 to calcuate optimal value)")
	}
	var sectorsPerCluster []byte
	if args.SectorsPerCluster == 255 {
		//calculate ideal value
		//CURRENTLY HARDCODED
		sectorsPerCluster = []byte{byte(0x04)}

	} else {
		sectorsPerCluster = []byte{byte(args.SectorsPerCluster)}
	}

	reservedSectors := []byte{byte(args.ReservedSectors & 0x00FF), byte((uint(args.ReservedSectors) & 0xFF00) >> 8)}
	numberOfFats := []byte{byte(args.NumberOfFats & 0x00FF), byte((uint(args.NumberOfFats) & 0xFF00) >> 8)}
	rootEntries := []byte{byte(args.NumberOfRootEntries & 0x00FF), byte((uint(args.NumberOfRootEntries) & 0xFF00) >> 8)}

	mediaDescriptor := []byte{byte(args.MediaDescriptor)}
	sectorsPerFat := []byte{byte(args.SectorsPerFat & 0x00FF), byte((uint(args.SectorsPerFat) & 0xFF00) >> 8)}
	sectorsPerTrack := []byte{byte(args.SectorsPerTrack & 0x00FF), byte((uint(args.SectorsPerTrack) & 0xFF00) >> 8)}
	numberOfHeads := []byte{byte(args.NumberOfHeads & 0x00FF), byte((uint(args.NumberOfHeads) & 0xFF00) >> 8)}

	hiddenSectors := []byte{byte(args.HiddenSectors & 0x000000FF), byte((args.HiddenSectors & 0x0000FF00) >> 8), byte((args.HiddenSectors & 0x00FF0000) >> 16), byte((args.HiddenSectors & 0xFF000000) >> 24)}

	//small/large number of sectors
	var numberOfSmallSectors []byte
	var numberOfLargeSectors []byte
	if imgSizeBytes < 33554432 {
		numberOfLargeSectors = []byte{0x00, 0x00, 0x00, 0x00}
	} else {
		numberOfSmallSectors = []byte{0x00, 0x00}
	}

	return nil, nil
}
*/
