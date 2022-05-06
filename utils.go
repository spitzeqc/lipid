package lipid

import (
	"errors"
	"log"
	"os"
	"strings"
)

func (f *fat16) findOffset(dirOffset int64, path string) int64 {
	clusterNumberOffset := int64(0x1A + dirOffset)
	clusterN := getValue(f.File, offsetObject{clusterNumberOffset, 2})

	clusterSize := f.ClusterSize

	//get offset of directory cluster
	clusterOffset := (f.RegionOffsets.DataRegion.Offset) + ((clusterN - 2) * clusterSize)

	//if directory cluster is the root directory, adjust values
	if dirOffset == f.RegionOffsets.RootDirRegion.Offset {
		clusterOffset = f.RegionOffsets.RootDirRegion.Offset
		clusterSize = f.RegionOffsets.RootDirRegion.Length
	}

	for i := int64(0x00); i < clusterSize; {
		off := clusterOffset + i

		//check if entry has been deleted or is free
		temp := getValue(f.File, offsetObject{off, 1})
		if temp == 0xE5 || temp == 0x00 {
			i += 32
			continue
		}

		//check for LFN
		if getValue(f.File, offsetObject{off + 0xB, 1}) == 0x0F {
			unicodeOffsets := fat16UnicodeReverseOffsets

			reverseFileName := ""
			lfnLength := getValue(f.File, offsetObject{off, 1}) & 0x3F
			//go through each chain link in LFN chain
			for j := int64(0); j < lfnLength; j++ {
				//access each character offset
				for _, o := range unicodeOffsets {
					//calculate char offset
					p := int64(j*32) + off + o
					charBytes, _ := readBytes(f.File, p, 2, true)

					r := rune(btoi64(&charBytes))
					if r == 0xffff || r == 0 {
						continue
					}
					reverseFileName += string(r)
				}
			}

			//flip file name around
			fileName := ""
			for c := range reverseFileName {
				fileName += string(reverseFileName[len(reverseFileName)-c-1])
			}

			//check if file name matches path
			if fileName == path {
				return ((lfnLength + 2) * 32) + clusterOffset
			}

			//i += (lfnLength + 1) * 32 //original, skips over non-LFN entry in a LFN
			i += lfnLength * 32
		} else {
			//not a LFN
			//read file name
			byteArrayName, _ := readBytes(f.File, off, 8, false)
			fileNameBytes := make([]byte, 0)

			for j := range byteArrayName {
				//remove any blank spaces from name
				if byteArrayName[j] > 0x20 {
					fileNameBytes = append(fileNameBytes, byteArrayName[j])
				} else if byteArrayName[j] == 0x05 {
					fileNameBytes = append(fileNameBytes, 0x35)
				}
			}
			//read file extension
			byteArrayExt, _ := readBytes(f.File, off+8, 3, false)
			fileExtBytes := make([]byte, 0)

			for j := range byteArrayExt {
				//remove any blank spaces from extension
				if byteArrayExt[j] > 0x20 {
					fileExtBytes = append(fileExtBytes, byteArrayExt[j])
				} else if byteArrayExt[j] == 0x05 {
					fileExtBytes = append(fileExtBytes, 0x35)
				}
			}

			fileName := string(fileNameBytes)
			if len(fileExtBytes) > 0 {
				fileName += "." + string(fileExtBytes)
			}

			if fileName == path {
				return i + clusterOffset
			}

			i += 32
		}
	}

	return -1
}

//makes an entry
func (f *fat16) makeEntry(name string) (int64, error) {
	//remove path seperator character if needed
	if name[len(name)-1] == '/' {
		name = name[:len(name)-1]
	}

	//check if name has been provided
	if name == "" {
		return -1, errors.New("you need to specify an entry name")
	}

	//CD TO PATH
	//generate path name
	sName := strings.Split(name, "/")
	dirPathName := ""
	if len(sName) > 1 {
		for _, e := range sName[:len(sName)-1] {
			dirPathName += e + "/"
		}
	} else {
		dirPathName = sName[0]
	}

	fileName := sName[len(sName)-1]
	//get path dir offset
	var workingDirOffset int64
	if len(sName) > 1 && len(dirPathName) > 0 {
		cdTemp := f.CurrentDirOffset
		log.Printf("%x\n", cdTemp)
		err := f.ChangeDir(dirPathName)
		if err != nil {
			message := dirPathName + " is not a valid path"
			return -1, errors.New(message)
		}
		workingDirOffset = f.CurrentDirOffset
		log.Printf("%x\n", workingDirOffset)
		//reset currentdiroffset
		f.CurrentDirOffset = cdTemp
		log.Printf("%x\n", f.CurrentDirOffset)
	} else {
		workingDirOffset = f.CurrentDirOffset
	}
	//check if entry with this name already exists
	if f.findOffset(workingDirOffset, fileName) != -1 {
		return -1, errors.New("entry with this name already exists")
	}

	//CREATE FAT ENTRY NAME
	//create non-long file name
	split := strings.Split(fileName, ".")

	regularName := ""
	regularExt := ""

	//find index of first non-'.' entry
	i := 0
	for i < len(split) && split[i] == "" {
		i++
	}
	//find index of last non-'.' entry
	j := len(split) - 1
	for j >= 0 && split[j] == "" {
		j--
	}

	//check if name will fit
	if len(split[i]) <= 8 {
		regularName = strings.ToUpper(split[i][:len(split[i])])
	} else {
		temp := []byte(strings.ToUpper(split[i][:8]))
		temp[6] = '~'
		temp[7] = '1'
		regularName = string(temp)
	}
	//check extension exists
	if i != j {
		//check if extension will fit
		if len(split[j]) <= 3 {
			regularExt = strings.ToUpper(split[j][:len(split[j])])
		} else {
			regularExt = strings.ToUpper(split[j][:3])
		}
	}

	//adjust name as needed
	temp := []byte(regularName)
	tempExt := ""
	if regularExt != "" {
		tempExt = "." + regularExt
	}

	for f.findOffset(workingDirOffset, (string(temp)+tempExt)) != -1 {
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
			return -1, errors.New("could not create entry with this name")
		}
	}
	regularName = string(temp)

	//LOCATE ENTRY OFFSET
	isLFN := (regularName + tempExt) != fileName
	//determine how many entries we need to use
	entries := (len(fileName) / 13)
	if len(fileName)%13 != 0 {
		entries++
	}
	if isLFN {
		entries++ //need to add one for the non-lfn entry
	}

	//determine size of search region
	var regionSize int64
	if workingDirOffset == f.RegionOffsets.RootDirRegion.Offset {
		regionSize = f.RegionOffsets.RootDirRegion.Length
	} else {
		regionSize = f.ClusterSize
	}

	//look through cluster for location of entry
	entryOffset := int64(-1)
	for i := 0; i < int(regionSize); i += 32 {
		found := true
		off := workingDirOffset + int64(i)
		//look for section with appropriate number of adjacent entries
		for j := 0; j < entries; j++ {
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
		return -1, errors.New("no space left in cluster")
	}

	//INSERT ENTRY
	//find entry location in FAT
	foundFatEntry := false
	fatEntry := int64(3)
	for i := int64(3); i*2 < f.RegionOffsets.FATRegion.Length; i++ {
		if getValue(f.File, offsetObject{f.RegionOffsets.FATRegion.Offset + (i * 2), 2}) == 0x0000 {
			foundFatEntry = true
			break
		}
		fatEntry++
	}
	if !foundFatEntry {
		return -1, errors.New("no space left in FAT")
	}

	//insert non-LFN entry
	tempOffset := entryOffset + (int64(entries)-1)*32
	//generate byte-array to write
	entryByteArray := make([]byte, 0)
	entryByteArray = append(entryByteArray, []byte(regularName)...)
	for len(entryByteArray) < 8 {
		entryByteArray = append(entryByteArray, 0x20)
	}
	entryByteArray = append(entryByteArray, []byte(regularExt)...)
	for len(entryByteArray) < 11 {
		entryByteArray = append(entryByteArray, 0x20)
	}
	//attribute, reserved, and creation
	for i := 0; i < 5; i++ {
		entryByteArray = append(entryByteArray, 0x00)
	}
	//creation date
	entryByteArray = append(entryByteArray, []byte{0x00, 0x21}...)
	//last access date
	entryByteArray = append(entryByteArray, []byte{0x00, 0x21}...)
	//reserved, last write time
	for i := 0; i < 4; i++ {
		entryByteArray = append(entryByteArray, 0x00)
	}
	//last write date
	entryByteArray = append(entryByteArray, []byte{0x00, 0x21}...)
	//starting cluster
	entryByteArray = append(entryByteArray, byte((fatEntry)&0x0000FFFF))
	entryByteArray = append(entryByteArray, byte((fatEntry)&0xFFFF0000))
	//file size
	for i := 0; i < 4; i++ {
		entryByteArray = append(entryByteArray, 0x00)
	}
	//write entry to cluster
	err := writeBytes(f.File, entryByteArray, tempOffset)
	if err != nil {
		return -1, err
	}
	//write entry to FAT(s)
	entryByteArray = []byte{0xFF, 0xFF}
	numOfFats := getValue(f.File, offsetObject{BootSector.FatCopies.Offset, BootSector.FatCopies.Length})
	for fat := int64(0); fat < numOfFats; fat++ {
		fatOffset := f.RegionOffsets.FATRegion.Offset + (fat * (f.RegionOffsets.FATRegion.Length / numOfFats))
		err = writeBytes(f.File, entryByteArray, fatOffset+(fatEntry*2))
		if err != nil {
			return -1, err
		}
	}

	//insert LFN entry
	if isLFN {
		//generate checksum
		sumName := regularName
		for len(sumName) < 8 {
			sumName += " "
		}
		sumName += regularExt
		for len(sumName) < 11 {
			sumName += " "
		}
		cSum := lfnCheck(sumName)
		log.Printf("%x\n", cSum)

		//add LFN entries
		for e := 0; e < entries-1; e++ {
			lfnOff := (entryOffset + int64((entries)-2-e)*32)
			//clear out entry
			clearOutArray := make([]byte, 0)
			for c := 0; c < 32; c++ {
				clearOutArray = append(clearOutArray, 0x00)
			}
			writeBytes(f.File, clearOutArray, lfnOff)

			//write LFN flag
			writeBytes(f.File, []byte{0x0F}, lfnOff+0x0B)

			//write checksum
			writeBytes(f.File, []byte{cSum}, lfnOff+0x0D)

			//write ordinal field
			writeBytes(f.File, []byte{byte(e + 1)}, lfnOff)

			//write characters
			for i, c := range fat16UnicodeOffsets {
				index := i + e*13
				if !(index < len(fileName)) {
					writeBytes(f.File, []byte{0xFF, 0xFF}, lfnOff+c)
				} else {
					writeBytes(f.File, []byte{fileName[index]}, lfnOff+c)
				}
			}
		}

		//set last LFN entry
		finalByte := byte(getValue(f.File, offsetObject{entryOffset, 1})) | 0x40
		err = writeBytes(f.File, []byte{finalByte}, entryOffset)
		if err != nil {
			return -1, err
		}
	}

	return (entryOffset + (int64(entries)-1)*32), nil
}

//read file name from a given offset
func (f *fat16) readName(offset int64) string {
	//check if entry is free or deleted
	b, _ := readBytes(f.File, offset, 1, false)
	if b[0] == 0x00 || b[0] == 0xE5 {
		return ""
	}

	isLFNOffset := 0x0B
	b, _ = readBytes(f.File, offset+int64(isLFNOffset), 1, false)
	isLFN := b[0] == byte(0x0F)

	//Is LFN
	if isLFN {
		temp, _ := readBytes(f.File, offset, 1, false)
		lfnChainLength := temp[0] & 0x3F

		fileNameReversed := ""
		unicodeOffsets := fat16UnicodeReverseOffsets
		//go through lfn entries
		for i := byte(0); i < lfnChainLength; i++ {
			//go through each character (in reverse)
			for _, o := range unicodeOffsets {
				p := o + offset + int64(i*32)

				t, _ := readBytes(f.File, p, 2, true)

				j := btoi64(&t)
				if j == 0x0000 || j == 0xffff {
					continue
				}
				r := rune(j)
				fileNameReversed += string(r)
			}
		}
		//reverse file name
		fileName := ""
		for c := range fileNameReversed {
			fileName += string(fileNameReversed[len(fileNameReversed)-c-1])
		}
		if fileName != "" {
			return fileName
		}
	} else {
		//Is not LFN
		//read file name
		byteArrayName, _ := readBytes(f.File, offset, 8, false)
		fileNameBytes := make([]byte, 0)

		for j := range byteArrayName {
			//remove any blank spaces from name
			if byteArrayName[j] > 0x20 {
				fileNameBytes = append(fileNameBytes, byteArrayName[j])
			} else if byteArrayName[j] == 0x05 {
				fileNameBytes = append(fileNameBytes, 0x35)
			}
		}
		//read file extension
		byteArrayExt, _ := readBytes(f.File, offset+8, 3, false)
		fileExtBytes := make([]byte, 0)

		for j := range byteArrayExt {
			//remove any blank spaces from extension
			if byteArrayExt[j] > 0x20 {
				fileExtBytes = append(fileExtBytes, byteArrayExt[j])
			} else if byteArrayExt[j] == 0x05 {
				fileExtBytes = append(fileExtBytes, 0x35)
			}
		}

		fileName := string(fileNameBytes)
		if len(fileExtBytes) > 0 {
			fileName += "." + string(fileExtBytes)
		}
		return fileName
	}
	return ""
}

//create fileSystemOffsetStructure for a given file in HEX offsets
func getRegionData(file *os.File) fileSystemOffsetStruct {
	bytesPerSector := getValue(file, BootSector.BytesPerSector)
	sectorsPerFat := getValue(file, BootSector.SectorsPerFat)
	numberOfFats := getValue(file, BootSector.FatCopies)
	rootEntriesCount := getValue(file, BootSector.RootEntries)
	reservedSectorsCount := getValue(file, BootSector.ReservedSectors)
	totalNumberOfSectors := getValue(file, BootSector.SmallSectors) + getValue(file, BootSector.LargeSectors)

	//values for return struct
	reservedRegionStart := VOLUME_START
	reservedRegionSize := reservedRegionStart + (reservedSectorsCount * bytesPerSector)

	fatRegionStart := reservedRegionStart + reservedRegionSize
	fatRegionSize := (numberOfFats * sectorsPerFat) * bytesPerSector

	rootDirRegionStart := fatRegionStart + fatRegionSize
	rootDirRegionSize := rootEntriesCount * int64(32)

	dataRegionStart := rootDirRegionStart + rootDirRegionSize
	dataRegionSize := (totalNumberOfSectors * bytesPerSector) - (reservedRegionSize + fatRegionSize + rootDirRegionSize)

	return fileSystemOffsetStruct{
		offsetObject{reservedRegionStart, reservedRegionSize},
		offsetObject{fatRegionStart, fatRegionSize},
		offsetObject{rootDirRegionStart, rootDirRegionSize},
		offsetObject{dataRegionStart, dataRegionSize},
	}
}

func lfnCheck(sfn string) byte {
	checksum := byte(0)
	for i := 0; i < 11; i++ {
		temp := byte(0)
		if (checksum & 0x01) == 0x01 {
			temp = 0x80
		}
		checksum = temp + byte(checksum>>1) + sfn[i]
	}
	return checksum
}
