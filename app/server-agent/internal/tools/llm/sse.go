package llm

import (
	"bufio"
	"bytes"
	"io"
)

// ReadSSE reads a Server-Sent Events stream from r, calling onData for each
// "data: ..." line. It stops without error when the stream ends or when
// the "[DONE]" sentinel is received.
func ReadSSE(r io.Reader, onData func([]byte) error) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}
		payload := bytes.TrimPrefix(line, []byte("data: "))
		if bytes.Equal(payload, []byte("[DONE]")) {
			return nil
		}
		if err := onData(payload); err != nil {
			return err
		}
	}
	return scanner.Err()
}
