package textbook

import "os"

func readFile(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
