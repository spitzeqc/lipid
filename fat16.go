package lipid

import (
	"errors"
	"io/ioutil"
	"log"
	"os"
	"strings"
)

const VOLUME_START int64 = 0x0

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
	ClusterSize      int64
}

//open a fat16 image
func OpenFat16Image(path string) (*Fat16, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0755)
	if err != nil {
		return &Fat16{nil}, err
	}

	hexData := getRegionData(file)
	f := &Fat16{&fat16{
		File:             file,
		RegionOffsets:    hexData,
		CurrentDirOffset: hexData.RootDirRegion.Offset,
		ClusterSize:      getValue(file, BootSector.BytesPerSector) * getValue(file, BootSector.SectorsPerCluster),
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

	//fatNumOffset+(fatSectorN*2)/bytesPerSector
	//firstSector := (f.RegionOffsets.DataRegion.Offset) + ((clusterN - 2) * sectorsPerCluster * bytesPerSector)
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
		//log.Printf("Cluster: %x\n", cluster)
		//log.Printf("Offset: %x\n", clusterOffset)
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
	pathSegments := strings.Split(newPath, "/")
	workingOffset := f.CurrentDirOffset
	if pathSegments[0] == "" {
		workingOffset = f.RegionOffsets.RootDirRegion.Offset
		pathSegments = pathSegments[1:]
	}

	for _, p := range pathSegments {
		if p == "" {
			continue
		}

		//get offset of given file
		temp := f.findOffset(workingOffset, p)
		if temp == -1 {
			return errors.New("Error: " + newPath + " is not a valid path!")
		}
		//make sure "directory" is not actually a file
		if (getValue(f.File, offsetObject{temp + directoryEntryOffsets.AttributeByte.Offset, directoryEntryOffsets.AttributeByte.Length}) & 0x10) != 0x10 {
			return errors.New("Error: " + newPath + " is not a directory!")
		}
		workingOffset = f.GetClusterOffset(getValue(f.File, offsetObject{temp + directoryEntryOffsets.StartingCluster.Offset, directoryEntryOffsets.StartingCluster.Length}))

	}
	f.CurrentDirOffset = workingOffset
	return nil
}

//list the dir at provided offset
func (f *fat16) ListDir() []string {
	returnSlice := make([]string, 0)
	dirOffset := f.CurrentDirOffset
	if dirOffset == f.RegionOffsets.RootDirRegion.Offset {
		i := int64(0x00)
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
		return returnSlice
	}

	//check if "Dir" is actually a directory
	attributeByte := getValue(f.File, offsetObject{directoryEntryOffsets.AttributeByte.Offset + dirOffset, directoryEntryOffsets.AttributeByte.Length})
	if (attributeByte & 0x10) == 0x10 {
		clusterNumberOffset := int64(0x1A + dirOffset)
		clusterN := getValue(f.File, offsetObject{clusterNumberOffset, 2})
		sectorsPerCluster := getValue(f.File, BootSector.SectorsPerCluster)
		bytesPerSector := getValue(f.File, BootSector.BytesPerSector)
		clusterSize := f.ClusterSize

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
		return returnSlice
	}
	return make([]string, 0)
}

func (f *fat16) MakeDir(name string) error {
	//make entry
	entryOff, err := f.makeEntry(name)
	if err != nil {
		return err
	}
	log.Printf("%x\n", entryOff)
	//set bit 0x10 on attribute byte
	attrByte := byte(getValue(f.File, offsetObject{entryOff + 0x0B, 1})) | 0x10
	err = writeBytes(f.File, []byte{attrByte}, entryOff+0x0B)
	if err != nil {
		return err
	}

	//CREATE . AND .. ENTRIES
	//create '.' entry array
	childCluster := getValue(f.File, offsetObject{entryOff + 0x1A, 2})
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
