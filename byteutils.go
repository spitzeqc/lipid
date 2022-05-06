package lipid

import "os"

func getValue(file *os.File, offObj offsetObject) int64 {
	temp, _ := readBytes(file, offObj.Offset, offObj.Length, true)
	return btoi64(&temp)
}
func readBytes(file *os.File, offset int64, numBytes int64, swapEndian bool) ([]byte, error) {
	byteBuff := make([]byte, numBytes)
	_, err := file.Seek(offset, 0)
	if err != nil {
		return nil, err
	}
	_, err = file.Read(byteBuff)
	if err != nil {
		return nil, err
	}
	if swapEndian {
		swapEndianness(&byteBuff)
	}
	return byteBuff, nil
}

//reverse the order of a byte array
func swapEndianness(b *[]byte) {
	arrayLength := len(*b)
	runLength := len(*b) / 2
	a := *b
	for i := 0; i < runLength; i++ {
		temp := a[i]
		a[i] = a[arrayLength-i-1]
		a[arrayLength-i-1] = temp
	}
}

//convert a byte array to int64
func btoi64(b *[]byte) int64 {
	var r int64 = 0x00
	for _, e := range *b {
		r *= 0x100
		r += int64(e)
	}
	return r
}

func writeBytes(file *os.File, bytes []byte, offset int64) error {
	_, err := file.Seek(offset, 0)
	if err != nil {
		return err
	}
	_, err = file.Write(bytes)
	if err != nil {
		return err
	}
	return nil
}
