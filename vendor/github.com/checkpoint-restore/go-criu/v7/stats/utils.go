package stats

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"

	"google.golang.org/protobuf/proto"
)

func readStatisticsFile(imgDir *os.File, fileName string) (*StatsEntry, error) {
	buf, err := os.ReadFile(filepath.Join(imgDir.Name(), fileName))
	if err != nil {
		return nil, err
	}

	if binary.LittleEndian.Uint32(buf[PrimaryMagicOffset:SecondaryMagicOffset]) != ImgServiceMagic {
		return nil, errors.New("primary magic not found")
	}

	if binary.LittleEndian.Uint32(buf[SecondaryMagicOffset:SizeOffset]) != StatsMagic {
		return nil, errors.New("secondary magic not found")
	}

	payloadSize := binary.LittleEndian.Uint32(buf[SizeOffset:PayloadOffset])

	st := &StatsEntry{}
	if err := proto.Unmarshal(buf[PayloadOffset:PayloadOffset+payloadSize], st); err != nil {
		return nil, err
	}

	return st, nil
}

func CriuGetDumpStats(imgDir *os.File) (*DumpStatsEntry, error) {
	st, err := readStatisticsFile(imgDir, StatsDump)
	if err != nil {
		return nil, err
	}

	return st.GetDump(), nil
}

func CriuGetRestoreStats(imgDir *os.File) (*RestoreStatsEntry, error) {
	st, err := readStatisticsFile(imgDir, StatsRestore)
	if err != nil {
		return nil, err
	}

	return st.GetRestore(), nil
}
