package p9log

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// const (
// 	thresholdSize = 1024 // 1KB
// )

type SizeLimitedFile struct {
	fileName    string
	file        *os.File
	size        int64
	maxFileSize int64
	writer      io.WriteCloser
}

func NewSizeLimitedFile(fileName string, maxFileSize int64) (*SizeLimitedFile, error) {
	f, err := os.Create(fileName)
	if err != nil {
		return nil, err
	}
	return &SizeLimitedFile{
		fileName:    fileName,
		file:        f,
		size:        0,
		maxFileSize: maxFileSize,
		writer:      f,
	}, nil
}

func (s *SizeLimitedFile) Write(p []byte) (n int, err error) {
	n, err = s.file.Write(p)
	s.size += int64(n)
	if s.size >= s.maxFileSize {
		if err := s.Rotate(); err != nil {
			return n, err
		}
	}
	return n, err
}

func (s *SizeLimitedFile) Rotate() error {
	if closer, ok := s.writer.(io.Closer); ok {
		closer.Close()
	}
	currentTime := time.Now()
	timestamp := currentTime.Format("2006-01-02-15-04-05")
	newFileName := filepath.Join(filepath.Dir(s.fileName), fmt.Sprintf("log-%s.txt", timestamp))
	f, err := os.Create(newFileName)
	if err != nil {
		return err
	}
	s.file = f
	s.size = 0
	s.fileName = newFileName
	return nil
}

func LoggerFile(path string, thresholdSize int64) io.Writer {
	logDir := filepath.Dir(path)
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return nil
		}
	}
	logFileName := filepath.Join(logDir, "log.txt")
	logFile, err := NewSizeLimitedFile(logFileName, thresholdSize)
	if err != nil {
		return nil
	}
	return &SizeLimitedFileWriter{SizeLimitedFile: logFile}
}

type SizeLimitedFileWriter struct {
	*SizeLimitedFile
}

func (w *SizeLimitedFileWriter) Write(p []byte) (n int, err error) {
	return w.SizeLimitedFile.Write(p)
}

func (w *SizeLimitedFileWriter) Close() error {
	// Ensure that the log file is closed and any resources are released
	if closer, ok := w.SizeLimitedFile.writer.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
