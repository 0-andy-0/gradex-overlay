package main

/*
 * Read a folder of anonymised script PDFs and:
 * (1) create a folder for each script, containing a jpg of each page
 * (2) create an XML file for each script, in the format required by RM Assessor
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
	
	"github.com/bsipos/thist"
	"github.com/timdrysdale/pool"
	//unicommon "github.com/timdrysdale/unipdf/v3/common"
)


func init() {
	// Debug log level.
	//unicommon.SetLogger(unicommon.NewConsoleLogger(unicommon.LogLevelInfo))
}

func main() {

    var courseCode string
    flag.StringVar(&courseCode, "course", "MATH00000", "to appear in the header")
	
	var inputDir string
    flag.StringVar(&inputDir, "inputdir", "input_dir", "path of the folder containing the PDF files to be processed")
	
	var outputDir string
    flag.StringVar(&outputDir, "outputdir", "output_dir", "path of the folder where output files should go")
	
	xmlOnly := flag.Bool("xmlonly", false, "generate only XML, no JPG? (true/false)")
		
	flag.Parse()

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
	max_pages := 0
	filepath.Walk(inputDir, func(path string, f os.FileInfo, _ error) error {
		if !f.IsDir() {
			if filepath.Ext(f.Name()) == ".pdf" {
				num_pages, _ := countPages(inputDir+"/"+f.Name())
				if num_pages > max_pages {
					max_pages = num_pages
				}
				input_pdfs = append(input_pdfs, f.Name())
			}
		}
		return nil
	})
	fmt.Println("input files: ",len(input_pdfs))
	fmt.Println("maximum page length: ",max_pages)
		

	N := len(input_pdfs)

	pcChan := make(chan int, N)

	tasks := []*pool.Task{}

	for i := 0; i < N; i++ {

		inputPDF := input_pdfs[i]
		
		newtask := pool.NewTask(func() error {
			pc, err := doOneDoc(inputPDF, inputDir, outputDir, courseCode, max_pages, xmlOnly)
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

}

func doOneDoc(filename string, inputDir string, outputDir string, courseCode string, max_pages int, xmlOnly *bool) (int, error) {

	suffix := filepath.Ext(filename)
	basename := strings.TrimSuffix(filename, suffix)
	
	inputPath := inputDir+"/"+filename
	if strings.ToLower(suffix) != ".pdf" {
		return 0, errors.New(fmt.Sprintf("%s does not appear to be a pdf", inputPath))
	}

	// need page count to find the jpeg files again later
	numPages, err := countPages(inputPath)

	// render to images
	jpegPath := outputDir+"/"+basename
	err = ensureDir(jpegPath)
	if err != nil {
		return 0, err
	}
	jpegFileOption := fmt.Sprintf("%s/%s_%%04d.jpg", jpegPath, basename)

	_, err = os.Open(inputPath)
	if err != nil {
		fmt.Println("Can't open pdf: ", inputPath, err)
		os.Exit(1)
	}
	fmt.Println("Reading PDF: ",inputPath)

	if !*xmlOnly {
		err = convertPDFToJPEGs(inputPath, jpegPath, jpegFileOption)
		if err != nil {
			return 0, err
		}
	}
	
	// TODO - create the XML and store it in outputDir+"/"+basename
	candidate_xml := makeXML(basename, numPages, courseCode, max_pages)
    f, err := os.Create(jpegPath+".xml")
    check(err)
    defer f.Close()
	_, err = f.WriteString(candidate_xml)
    check(err)
	
	return numPages, nil

}

func makeXML(ExamNo string, pages int, courseCode string, max_pages int) (string) {

	base_image_xml := `<Image>
            <PageNo>INT</PageNo>
            <ImageType>jpeg</ImageType>
            <ImagePath>EXAMNO\EXAMNO_FORMATINT.jpg</ImagePath>
        </Image>
`
	image_xml := ""
	for i := 1; i <= pages; i++ {
		this_image := strings.ReplaceAll(base_image_xml, "FORMATINT", fmt.Sprintf("%04d", i))
		this_image = strings.ReplaceAll(this_image, "INT", fmt.Sprintf("%d", i))
		image_xml = image_xml + this_image
	}
	if pages < max_pages {
		for i := pages+1; i <= max_pages; i++ {
			this_image := strings.ReplaceAll(base_image_xml, "EXAMNO\\EXAMNO_FORMATINT", "blankpage")
			this_image = strings.ReplaceAll(this_image, "INT", fmt.Sprintf("%d", i))
			image_xml = image_xml + this_image
		}
	}

	overall_xml := `<?xml version="1.0" encoding="UTF-8"?>
<CandidateScript>
    <QuestionPaperBarcode>COURSECODE</QuestionPaperBarcode>
    <CandidateName>EXAMNO</CandidateName>
    <UCI>EXAMNO</UCI>
    <ScanBatchID>SID0</ScanBatchID>
    <ScanScriptID>1SCANSCRIPTID</ScanScriptID>
    <ScanDate>01/05/2020</ScanDate>
    <AtypicalStatus>Normal</AtypicalStatus>
    <RescanRequestID></RescanRequestID>
    <ScannedCentreCandidateNo>EXAMNO</ScannedCentreCandidateNo>
    <ScannedCentreNum>UoESoM</ScannedCentreNum>
    <AdditionalInformation></AdditionalInformation>
    <Images>
        IMAGEXML
    </Images>
</CandidateScript>`

	overall_xml = strings.ReplaceAll(overall_xml, "COURSECODE", courseCode)
	overall_xml = strings.ReplaceAll(overall_xml, "IMAGEXML", image_xml)
	overall_xml = strings.ReplaceAll(overall_xml, "EXAMNO", ExamNo)
	overall_xml = strings.ReplaceAll(overall_xml, "SCANSCRIPTID", ExamNo[1:])
	
	return overall_xml

}

	
func check(e error) {
    if e != nil {
        panic(e)
    }
}