package filesystems

import "errors"

func CheckFSPath(path string, expectedFilesystem int64, expectedFilesystemName string) error {
	return errors.New("Implement me")
}
