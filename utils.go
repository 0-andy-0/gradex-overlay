package main

import (
	"os"

	pdf "github.com/timdrysdale/unipdf/v3/model"
)

// pr-pal @ https://stackoverflow.com/questions/37932551/mkdir-if-not-exists-using-golang
func ensureDir(dirName string) error {

	err := os.Mkdir(dirName, 0700) //probably umasked with 22 not 02

	os.Chmod(dirName, 0700)

	if err == nil || os.IsExist(err) {
		return nil
	} else {
		return err
	}

}

func countPages(inputPath string) (int, error) {

	numPages := 0

	f, err := os.Open(inputPath)
	if err != nil {
		return numPages, err
	}

	pdfReader, err := pdf.NewPdfReader(f)
	if err != nil {
		return numPages, err
	}

	defer f.Close()

	isEncrypted, err := pdfReader.IsEncrypted()
	if err != nil {
		return numPages, err
	}

	if isEncrypted {
		_, err = pdfReader.Decrypt([]byte(""))
		if err != nil {
			return numPages, err
		}
	}

	numPages, err = pdfReader.GetNumPages()
	if err != nil {
		return numPages, err
	}

	return numPages, nil

}
