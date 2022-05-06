package lipid

type offsetObject struct {
	Offset int64 //offset of object
	Length int64 //length of object (number of bytes)
}

type bootSectorOffsetsStruct struct {
	GotoBootstrapCode offsetObject
	OSName            offsetObject
	BytesPerSector    offsetObject
	SectorsPerCluster offsetObject
	ReservedSectors   offsetObject
	FatCopies         offsetObject
	RootEntries       offsetObject
	SmallSectors      offsetObject
	MediaDescriptor   offsetObject
	SectorsPerFat     offsetObject
	SectorsPerTrack   offsetObject
	NumberOfHeads     offsetObject
	HiddenSectors     offsetObject
	LargeSectors      offsetObject
	DriveNumber       offsetObject
	Reserved          offsetObject
	ExtBootSig        offsetObject
	VolumeSerialNum   offsetObject
	VolumeLabel       offsetObject
	FileSystemType    offsetObject
	BootstrapCode     offsetObject
	BootSectorSig     offsetObject
}
type directoryEntryOffsetsStruct struct {
	Filename          offsetObject
	FilenameExtension offsetObject
	AttributeByte     offsetObject
	Reserved          offsetObject
	Creation          offsetObject
	CreationTime      offsetObject
	CreationDate      offsetObject
	LastAccessDate    offsetObject
	ReservedFat32     offsetObject
	LastWriteTime     offsetObject
	LastWriteDate     offsetObject
	StartingCluster   offsetObject
	FileSize          offsetObject
}
type fileSystemOffsetStruct struct {
	ReservedRegion offsetObject
	FATRegion      offsetObject
	RootDirRegion  offsetObject
	DataRegion     offsetObject
}

//Add these offsets to offset of directory (is this a FAT16 only structure?)
var directoryEntryOffsets = directoryEntryOffsetsStruct{
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

//Offset structures
var BootSector = bootSectorOffsetsStruct{
	offsetObject{0x00, 3},   //GotoBootstrapCode
	offsetObject{0x03, 8},   //OSName
	offsetObject{0x0B, 2},   //BytesPerSector
	offsetObject{0x0D, 1},   //SectorsPerCluster
	offsetObject{0x0E, 2},   //ReservedSectors
	offsetObject{0x10, 1},   //FatCopies
	offsetObject{0x11, 2},   //RootEntries
	offsetObject{0x13, 2},   //SmallSectorCount
	offsetObject{0x15, 1},   //MediaDescriptor
	offsetObject{0x16, 2},   //SectorsPerFat
	offsetObject{0x18, 2},   //SectorsPerTrack
	offsetObject{0x1A, 2},   //NumberOfHeads
	offsetObject{0x1C, 4},   //HiddenSectors
	offsetObject{0x20, 4},   //LargeSectorCount
	offsetObject{0x24, 1},   //DriveNumber
	offsetObject{0x25, 1},   //Reserved
	offsetObject{0x26, 1},   //ExtBootSig
	offsetObject{0x27, 4},   //VolumeSerialNum
	offsetObject{0x2B, 11},  //VolumeLabel
	offsetObject{0x36, 8},   //FileSystemType
	offsetObject{0x3E, 448}, //BootstrapCode
	offsetObject{0x1FE, 2},  //BootSectorSig (AA 55)
}

var fat16UnicodeReverseOffsets = []int64{
	0x1E,
	0x1C,
	0x18,
	0x16,
	0x14,
	0x12,
	0x10,
	0x0E,
	0x09,
	0x07,
	0x05,
	0x03,
	0x01,
}

var fat16UnicodeOffsets = []int64{
	0x01,
	0x03,
	0x05,
	0x07,
	0x09,
	0x0E,
	0x10,
	0x12,
	0x14,
	0x16,
	0x18,
	0x1C,
	0x1E,
}
