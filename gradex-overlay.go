package main

/*
 * Add a cover page to a PDF file
 * Generates cover page then merges, including form field data (AcroForms).
 *
 * Run as: gradex-coverpage <barefile>.pdf
 *
 * outputs: <barefile>-covered.pdf (using internally generated cover page)
 *
 * Adapted from github.com/unidoc/unipdf-examples/pages/pdf_merge_advanced.go
 *
 *
 */

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"flag"
	"io/ioutil"
	
	"github.com/gocarina/gocsv"
	"github.com/bsipos/thist"
	"github.com/timdrysdale/parsesvg"
	"github.com/timdrysdale/pdfcomment"
	"github.com/timdrysdale/pool"
	unicommon "github.com/timdrysdale/unipdf/v3/common"
	pdf "github.com/timdrysdale/unipdf/v3/model"
)


func init() {
	// Debug log level.
	unicommon.SetLogger(unicommon.NewConsoleLogger(unicommon.LogLevelInfo))
}

func main() {

    var courseCode string
    flag.StringVar(&courseCode, "course", "MATH00000", "to appear in the header")
	
    var examDiet string
    flag.StringVar(&examDiet, "diet", "April 2020", "to appear in the header")
	
	var partsAndMarksCSV string
    flag.StringVar(&partsAndMarksCSV, "parts", "", "path to optional csv showing the question parts and associated marks")
	
    var markerID string
    flag.StringVar(&markerID, "marker", "", "optionally pre-fill the marker initials")
	
	var layoutSvg string
    flag.StringVar(&layoutSvg, "layout", "som/layout.svg", "svg file showing the overall layout of the different spreads")
	
	var spreadName string
    flag.StringVar(&spreadName, "spread", "mark", "the spread to add (e.g. mark/check/scrutiny)")
	
	var inputDir string
    flag.StringVar(&inputDir, "inputdir", "input_dir", "path of the folder containing the PDF files to be processed")
	
	var outputDir string
    flag.StringVar(&outputDir, "outputdir", "output_dir", "path of the folder where output files should go")
		
	flag.Parse()

	
	
	fmt.Println("Hello world")
	
	svgBytes, err := ioutil.ReadFile("test/sidebar-312pt-mark-flow.svg")
	if err != nil {
		fmt.Sprintf("Entity: error opening svg file sidebar-312pt-mark-flow")
	}

	ladder, err := parsesvg.DefineLadderFromSVG(svgBytes)

	
	
	fmt.Println(ladder)
	
	
	fmt.Println("Hello")
	fmt.Println(partsAndMarksCSV)
	
	
	// Deal with parts and marks
	partsinfo := getPartsAndMarks("parts_and_marks.csv")
	
	for _, part := range partsinfo {
		fmt.Println("Part: ",part.Part)
		fmt.Println("   ",part.Marks, " marks")
		
	}
	
	fmt.Println(partsinfo)
	
	
	
	//os.Exit(1)
	
	
	//
	// Tim's stuff from here on
	//
	
	if len(os.Args) < 2 {
		fmt.Printf("Requires two arguments: layout spread input_path[s]\n")
		fmt.Printf("Usage: gradex-overlay.exe layout spread input-*.pdf\n")
		os.Exit(0)
	}

	//layoutSvg := os.Args[1]

	//spreadName := os.Args[2]

	var inputPath [1]string

	inputPath[0] = "demo.pdf"//os.Args[3:] // TODO change to filewalk for cross platform!!

	suffix := filepath.Ext(inputPath[0])

	// sanity check
	if suffix != ".pdf" {
		fmt.Printf("Error: input path must be a .pdf\n")
		os.Exit(1)
	}

	N := len(inputPath)

	pcChan := make(chan int, N)

	tasks := []*pool.Task{}

	for i := 0; i < N; i++ {

		inputPDF := inputPath[i]
		spreadName := spreadName
		newtask := pool.NewTask(func() error {
			pc, err := doOneDoc(inputPDF, layoutSvg, spreadName, partsinfo, markerID)
			pcChan <- pc
			return err
		})
		tasks = append(tasks, newtask)
	}

	p := pool.NewPool(tasks, runtime.GOMAXPROCS(-1))

	closed := make(chan struct{})

	h := thist.NewHist(nil, "Page count", "fixed", 10, false)

	go func() {
	LOOP:
		for {
			select {
			case pc := <-pcChan:
				h.Update(float64(pc))
				fmt.Println(h.Draw())
			case <-closed:
				break LOOP
			}
		}
	}()

	p.Run()

	var numErrors int
	for _, task := range p.Tasks {
		if task.Err != nil {
			fmt.Println(task.Err)
			numErrors++
		}
	}
	close(closed)

}

func doOneDoc(inputPath, layoutSvg, spreadName string, parts_and_marks []*parsesvg.PaperStructure, markerID string) (int, error) {

	if strings.ToLower(filepath.Ext(inputPath)) != ".pdf" {
		return 0, errors.New(fmt.Sprintf("%s does not appear to be a pdf", inputPath))
	}

	// need page count to find the jpeg files again later
	numPages, err := countPages(inputPath)

	// render to images
	jpegPath := "./jpg"
	err = ensureDir(jpegPath)
	if err != nil {
		return 0, err
	}
	suffix := filepath.Ext(inputPath)
	basename := strings.TrimSuffix(inputPath, suffix)
	jpegFileOption := fmt.Sprintf("%s/%s%%04d.jpg", jpegPath, basename)

	f, err := os.Open(inputPath)
	if err != nil {
		fmt.Println("Can't open pdf")
		os.Exit(1)
	}

	pdfReader, err := pdf.NewPdfReader(f)
	if err != nil {
		fmt.Println("Can't read test pdf")
		os.Exit(1)
	}

	comments, err := pdfcomment.GetComments(pdfReader)

	f.Close()

	err = convertPDFToJPEGs(inputPath, jpegPath, jpegFileOption)
	if err != nil {
		return 0, err
	}

	// convert images to individual pdfs, with form overlay

	pagePath := "./pdf"
	err = ensureDir(pagePath)
	if err != nil {
		return 0, err
	}

	pageFileOption := fmt.Sprintf("%s/%s%%04d.pdf", pagePath, basename)

	mergePaths := []string{}

	// gs starts indexing at 1
	for imgIdx := 1; imgIdx <= numPages; imgIdx = imgIdx + 1 {

		// construct image name
		previousImagePath := fmt.Sprintf(jpegFileOption, imgIdx)
		pageFilename := fmt.Sprintf(pageFileOption, imgIdx)

		pageNumber := imgIdx - 1

		contents := parsesvg.SpreadContents{
			SvgLayoutPath:     layoutSvg,
			SpreadName:        spreadName,
			PreviousImagePath: previousImagePath,
			PageNumber:        pageNumber,
			PdfOutputPath:     pageFilename,
			Comments:          comments,
			Marker:			   markerID,
		}

		err := parsesvg.RenderSpreadExtra(contents, parts_and_marks)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		//save the pdf filename for the merge at the end
		mergePaths = append(mergePaths, pageFilename)
	}

	outputPath := fmt.Sprintf("%s-%s.pdf", basename, spreadName)
	err = mergePdf(mergePaths, outputPath)
	if err != nil {
		return 0, err
	}

	return numPages, nil

}

func getPartsAndMarks(csv_path string) []*parsesvg.PaperStructure {
	
	fmt.Println(csv_path)

	marksFile, err := os.OpenFile(csv_path, os.O_RDWR|os.O_CREATE, os.ModePerm)
	if err != nil {
		fmt.Println("File: ",csv_path, err)
		panic(err)
	}
	defer marksFile.Close()

	parts := []*parsesvg.PaperStructure{}
	if err := gocsv.UnmarshalFile(marksFile, &parts); err != nil {
		panic(err)
	}
	return parts
}
