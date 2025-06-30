package logging

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"

	"gitlab.com/tozd/go/errors"
)

func HandleDelimitedProxy(ctx context.Context, reader io.ReadCloser, writer io.Writer, delimiter rune) error {
	scanner := bufio.NewScanner(reader)
	scanner.Split(NewDelimiterSplitFunc(delimiter))
	for scanner.Scan() {
		fmt.Fprint(writer, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return errors.Errorf("scanner error: %w", err)
	}
	return nil
}

func replaceDelimiter(data []byte, delimiter rune) []byte {
	lenmin1 := len(data) - 1
	if len(data) > 0 && data[lenmin1] == byte(delimiter) {
		data = data[:lenmin1-1]
	}
	return data
}

func NewDelimiterSplitFunc(delimiter rune) bufio.SplitFunc {
	return func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		if i := bytes.IndexByte(data, byte(delimiter)); i >= 0 {
			// We have a full newline-terminated line.
			return i + 1, replaceDelimiter(data[0:i], delimiter), nil
		}
		// If we're at EOF, we have a final, non-terminated line. Return it.
		if atEOF {
			return len(data), replaceDelimiter(data, delimiter), nil
		}
		// Request more data.
		return 0, nil, nil
	}
}
