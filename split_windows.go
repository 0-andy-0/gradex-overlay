package main

import (
	"fmt"
	"os/exec"
)

// simplified https://github.com/catherinelu/evangelist/blob/master/server.go

func convertPDFToJPEGs(pdfPath string, jpegPath string, outputFile string) error {

	outputFileOption := fmt.Sprintf("-sOutputFile=%s", outputFile)

	cmd := exec.Command("gswin64c", "-dNOPAUSE", "-sDEVICE=jpeg", outputFileOption, "-dJPEGQ=90", "-r200", "-q", pdfPath,
		"-c", "quit")

	err := cmd.Run()
	if err != nil {
		fmt.Printf("gs command failed: %s\n", err.Error(), cmd.String())
		return err
	}

	return nil
}

// This worked
// gs -dNOPAUSE -sDEVICE=jpeg -sOutputFile=edited-%d.jpg -dJPEGQ=95 -r300 -q edited5-covered.pdf -c quit
