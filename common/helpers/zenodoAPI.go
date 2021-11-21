package helpers

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"os"
	"path/filepath"
	"time"

	"github.com/m7shapan/njson"
	"github.com/sirupsen/logrus"
)

type ZENODOResponse struct {
	BucketURL string 	`njson:"links.bucket"`
	DiscardURL string `njson:"links.discard"`
	PublishURL string `njson:"links.publish"`
	WebURL string 		`njson:"links.latest_html"`
}

type ZenodoRequestBody struct {
	Metadata ZenodoDeposition  	`json:"metadata"`
}

type Creator struct {
	Name string  	`json:"name"`
}

type ZenodoDeposition struct {
	Title 				string  		`json:"title"`
	UploadType 		string  		`json:"upload_type"`
	Description 	string  		`json:"description"`
	Creators			[]Creator		`json:"creators"`
}

type DatasetStruct struct {
  Managers  		[]string 				`json:"managers"`
	Datasets 			[]DatasetInfo 	`json:"datasets"`
}

type DatasetInfo struct {
	Date string `json:"date"`
	Url string `json:"url"`
}

const (
	// ZENODO_API = "https://zenodo.org/api/"
	ZENODO_API = "https://sandbox.zenodo.org/api/" // Development testing API
)

func ReleaseDataset() {
	zenodoToken := getEnvVariable("ZENODO_API_KEY")

	// Create Zip file for upload
	fmt.Println("Creating zip file...")
	zipFileName := time.Now().Format("02-01-2006") + "-dataset.zip"
	zipErr := zipSource("data/",zipFileName)
	if (zipErr != nil) {
		fmt.Println("Error creating zip file: %s", zipErr)
	}
	fmt.Println("Created zip file")


	// Create deposit (bucket) for the dataset
	endpoint := ZENODO_API + "deposit/depositions?access_token=" + zenodoToken
	bodyObject := ZenodoRequestBody{
		Metadata: ZenodoDeposition{
			Title: "DASEA " + time.Now().Format("02-01-2006"),
			UploadType: "dataset",
			Description: "A continuously updated dataset of software dependencies covering various package manager ecosystems. Read more on https://heyjoakim.github.io/DASEA/",
			Creators: []Creator{ { Name: "jhhi@itu.dk" }, { Name: "kols@itu.dk" }, { Name: "pebu@itu.dk" } },
		},
	}

	body, _ := json.Marshal(bodyObject)
	response := httpRequest("POST", endpoint, bytes.NewBuffer(body), "application/json")
	unmarshaledResponse, _ := unmarshalResponse(response)
	buckerURL := unmarshaledResponse.BucketURL
	publishURL := unmarshaledResponse.PublishURL
	fmt.Println("Generated Bucket on Zenodo")

	// Upload dataset to bucket
  uploadFileToBucket(zipFileName, buckerURL, zenodoToken)

	// Publish dataset
	webURL := publishDataset(publishURL, zenodoToken)
	fmt.Println(webURL)

	// Store web page url to DASEA datasets page
	updateDatasetPage(webURL)
}

func uploadFileToBucket(fileName string, buckerURL string, zenodoToken string) {
	// Preapare upload
	fmt.Println("Preparing file for upload...")
  buf := bytes.NewBuffer(nil)
	bodyWriter := multipart.NewWriter(buf)
	filename := "data/" + fileName
	fileWriter, err := bodyWriter.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		fmt.Println("Creating fileWriter: %s", err)
	}

	file, err := os.Open(filename)
	if err != nil {
		fmt.Println("Opening file: %s", err)
	}
	defer file.Close()

	if _, err := io.Copy(fileWriter, file); err != nil {
		fmt.Println("Buffering file: %s", err)
	}

	contentType := bodyWriter.FormDataContentType()
	bodyWriter.Close()

	fmt.Println("Begin uploading dataset to Zenodo...")

	// Upload file to bucket
	uploadEndpoint := buckerURL + "/" + time.Now().Format("02-01-2006") + "-sample.zip?access_token=" + zenodoToken
	res := httpRequest("PUT", uploadEndpoint, buf, contentType)
	_, uploadErr := unmarshalResponse(res)
	if (uploadErr == nil) {
		fmt.Println("Uploaded dataset to Zenodo")
	}
}

func publishDataset(publishURL string, zenodoToken string) string {
	publishEndpoint := publishURL + "?access_token=" + zenodoToken
	publishRes := httpRequest("POST", publishEndpoint, nil, "application/json")
	publishedDataset, publishErr := unmarshalResponse(publishRes)
	if (publishErr == nil) {
		fmt.Println("Published dataset on Zenodo")
	}
	return publishedDataset.WebURL
}

func updateDatasetPage(datasetUrl string) {
		filename := "docs/datasets.json"
    err := checkFile(filename)
    if err != nil {
        logrus.Error(err)
    }

    file, err := ioutil.ReadFile(filename)
    if err != nil {
        logrus.Error(err)
    }

    data := DatasetStruct{}

    // Here the magic happens!
    json.Unmarshal(file, &data)
    latestDataset := &DatasetInfo{
        Date: time.Now().Format("02-01-2006"),
				Url: datasetUrl,
    }

    data.Datasets = append([]DatasetInfo{*latestDataset}, data.Datasets...)

    // Preparing the data to be marshalled and written.
    dataBytes, err := json.Marshal(data)
    if err != nil {
        logrus.Error(err)
    }

    err = ioutil.WriteFile(filename, dataBytes, 0644)
    if err != nil {
        logrus.Error(err)
    }
}

func zipSource(source, target string) error {
    // 1. Create a ZIP file and zip.Writer
    f, err := os.Create(target)
    if err != nil {
        return err
    }
    defer f.Close()

    writer := zip.NewWriter(f)
    defer writer.Close()

    // 2. Go through all the files of the source
    return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
        if err != nil {
            return err
        }

        // 3. Create a local file header
        header, err := zip.FileInfoHeader(info)
        if err != nil {
            return err
        }

        // set compression
        header.Method = zip.Deflate

        // 4. Set relative path of a file as the header name
        header.Name, err = filepath.Rel(filepath.Dir(source), path)
        if err != nil {
            return err
        }
        if info.IsDir() {
            header.Name += "/"
        }

        // 5. Create writer for the file header and save content of the file
        headerWriter, err := writer.CreateHeader(header)
        if err != nil {
            return err
        }

        if info.IsDir() {
            return nil
        }

        f, err := os.Open(path)
        if err != nil {
            return err
        }
        defer f.Close()

        _, err = io.Copy(headerWriter, f)
        return err
    })
}

func unmarshalResponse(data []byte) (ZENODOResponse, error) {
	var r ZENODOResponse
	err := njson.Unmarshal(data, &r)
	return r, err
}
