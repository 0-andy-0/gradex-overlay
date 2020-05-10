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
	"regexp"
	
	"github.com/gocarina/gocsv"
	"github.com/bsipos/thist"
	"github.com/georgekinnear/parsesvg"
	"github.com/georgekinnear/gradex-extract/pdfextract"
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
    flag.StringVar(&examDiet, "diet", "Summer 2020", "to appear in the header")
	
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
	
	var tmpFiles string
    flag.StringVar(&tmpFiles, "tmpfiles", "discard", "whether to 'discard' or 'keep' the temporary jpg/pdf files from the process")
	
	redoAll := flag.Bool("redo_all", false, "regenerate all the output PDFs even if they exist? (true/false)")
		
	flag.Parse()

	fmt.Println(partsAndMarksCSV)
	// Deal with parts and marks
	partsinfo := []*parsesvg.PaperStructure{}
	if partsAndMarksCSV != "" {
		partsinfo = getPartsAndMarks(partsAndMarksCSV)
	}

	fmt.Println("Parts and marks: ",len(partsinfo))
	
	// Set some general facts about the scripts
	spread_contents := parsesvg.SpreadContents{CourseCode: courseCode, ExamDiet: examDiet, Marker: markerID}
	
	// Make sure outputDir exists
	err := ensureDir(outputDir)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	
	// Find all PDFs in the inputDir
	err = ensureDir(inputDir)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	var input_pdfs = []string{}
	filepath.Walk(inputDir, func(path string, f os.FileInfo, _ error) error {
		if !f.IsDir() {
			if filepath.Ext(f.Name()) == ".pdf" {
				input_pdfs = append(input_pdfs, f.Name())
			}
		}
		return nil
	})
	fmt.Println("input files: ",len(input_pdfs))
	

	N := len(input_pdfs)

	pcChan := make(chan int, N)

	tasks := []*pool.Task{}

	for i := 0; i < N; i++ {

		inputPDF := input_pdfs[i]
		spreadName := spreadName
		spread_contents_new := spread_contents
		//spread_contents_new.Candidate = strings.TrimSuffix(inputPDF, filepath.Ext(inputPDF))
		findexamno, _ := regexp.Compile("(B[0-9]{6})")
		spread_contents_new.Candidate = findexamno.FindStringSubmatch(inputPDF)[1]
		
		newtask := pool.NewTask(func() error {
			pc, err := doOneDoc(inputPDF, inputDir, outputDir, layoutSvg, spreadName, partsinfo, spread_contents_new, *redoAll)
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
				//fmt.Println(h.Draw())
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
	
	// Clean up the temporary jpgs/pdfs
	if tmpFiles == "discard" {
		os.RemoveAll(outputDir+"/jpg_pages")
		os.RemoveAll(outputDir+"/pdf_pages")
	}

}

func doOneDoc(filename, inputDir, outputDir, layoutSvg, spreadName string, parts_and_marks []*parsesvg.PaperStructure, initialContents parsesvg.SpreadContents, redoAll bool) (int, error) {

	suffix := filepath.Ext(filename)
	basename := strings.TrimSuffix(filename, suffix)
	outputPath := fmt.Sprintf(outputDir+"/%s-%s.pdf", basename, spreadName)
	
	// Unless regenerating all output files, check that this file does not already exist
	if !redoAll && fileExists(outputPath) {
		fmt.Println("Skipped existing: "+outputPath)
		return 0, nil
	}

	inputPath := inputDir+"/"+filename
	if strings.ToLower(filepath.Ext(filename)) != ".pdf" {
		return 0, errors.New(fmt.Sprintf("%s does not appear to be a pdf", inputPath))
	}

	// need page count to find the jpeg files again later
	numPages, err := countPages(inputPath)

	// render to images
	jpegPath := outputDir+"/jpg_pages"
	err = ensureDir(jpegPath)
	if err != nil {
		return 0, err
	}
	jpegFileOption := fmt.Sprintf("%s/%s_%%04d.jpg", jpegPath, basename)

	f, err := os.Open(inputPath)
	if err != nil {
		fmt.Println("Can't open pdf: ", inputPath, err)
		os.Exit(1)
	}
	//fmt.Println("Reading PDF: ",inputPath)

	pdfReader, err := pdf.NewPdfReader(f)
	if err != nil {
		fmt.Println("Can't read test pdf", inputPath, err)
		os.Exit(1)
	}

	comments, err := pdfcomment.GetComments(pdfReader)

	f.Close()
	
	// Get any existing form values that we care about
	form_values := pdfextract.ReadFormFromPDF(inputPath, false)
	fmt.Println(form_values)

	err = convertPDFToJPEGs(inputPath, jpegPath, jpegFileOption)
	if err != nil {
		return 0, err
	}

	// convert images to individual pdfs, with form overlay

	pagePath := outputDir+"/pdf_pages"
	err = ensureDir(pagePath)
	if err != nil {
		return 0, err
	}

	pageFileOption := fmt.Sprintf("%s/%s_%%04d.pdf", pagePath, basename)

	mergePaths := []string{}

	// gs starts indexing at 1
	for imgIdx := 1; imgIdx <= numPages; imgIdx = imgIdx + 1 {

		// construct image name
		previousImagePath := fmt.Sprintf(jpegFileOption, imgIdx)
		pageFilename := fmt.Sprintf(pageFileOption, imgIdx)

		pageNumber := imgIdx - 1

		contents := initialContents	
		contents.SvgLayoutPath = layoutSvg
		contents.SpreadName = spreadName
		contents.PreviousImagePath = previousImagePath
		contents.PageNumber = pageNumber
		contents.PdfOutputPath = pageFilename
		contents.Comments = comments
		if imgIdx == 1 && len(form_values)>0 {
			contents.PreviousFields = make(map[string]string)
			for _, field := range form_values {
				contents.PreviousFields[field.Field] = field.Value
			}
		}

		err := parsesvg.RenderSpreadExtra(contents, parts_and_marks)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		//save the pdf filename for the merge at the end
		mergePaths = append(mergePaths, pageFilename)
	}

	err = mergePdf(mergePaths, outputPath)
	if err != nil {
		return 0, err
	}
	
	fmt.Println("Created "+outputPath)
	
	return numPages, nil

}

func getPartsAndMarks(csv_path string) []*parsesvg.PaperStructure {
	
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

// fileExists checks if a file exists and is not a directory before we
// try using it to prevent further errors.
func fileExists(filename string) bool {
    info, err := os.Stat(filename)
    if os.IsNotExist(err) {
        return false
    }
    return !info.IsDir()
}
