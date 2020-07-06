package main

import (
	"C"
	"crypto/rand"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)
import "os/signal"

const maxUploadSize = 1024 * 1024 * 1024
const uploadPath = "/upload"
const downloadPath = "/download"

var server *http.Server

//export StartHTTPServer
func StartHTTPServer() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	downfolderPath := filepath.Join(getCurrentPath(), downloadPath)
	os.MkdirAll(downfolderPath, os.ModePerm)
	upfolderPath := filepath.Join(getCurrentPath(), uploadPath)
	os.MkdirAll(upfolderPath, os.ModePerm)

	// 一个通知退出的chan
	quit := make(chan os.Signal)
	signal.Notify(quit, os.Interrupt)
	go func() {
		// 接收退出信号
		<-quit
		if err := server.Close(); err != nil {
			log.Println("| hfs::StartHTTPServerClose server:", err)
		}
	}()

	server := &http.Server{
		Addr: ":8080",
	}

	http.HandleFunc("/upload", uploadFileHandler)
	http.HandleFunc("/upload/", uploadFileHandler)
	//http.HandleFunc("/download/", downloadFileHandler)
	log.Println("| hfs::StartHTTPServer download:", downfolderPath, ", upload:", upfolderPath)
	fs := http.FileServer(http.Dir(downfolderPath))
	http.Handle("/download/", http.StripPrefix("/download", fs))
	log.Print("| hfs::StartHTTPServer Started Listen Port:8080, use /upload/ for uploading files and /download/{fileName} for downloading")
	err := server.ListenAndServe()
	if err != nil {
		log.Println("| hfs::StartHTTPServer http server error: ", err)
	}
}

//export StopHTTPServer
func StopHTTPServer() {
	err := server.Shutdown(nil)
	if err != nil {
		log.Println("| hfs::StopHTTPServer shutdown the server err: ", err)
	}
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	downfolderPath := filepath.Join(getCurrentPath(), downloadPath)
	os.MkdirAll(downfolderPath, os.ModePerm)
	upfolderPath := filepath.Join(getCurrentPath(), uploadPath)
	os.MkdirAll(upfolderPath, os.ModePerm)

	http.HandleFunc("/upload/", uploadFileHandler)
	http.HandleFunc("/upload", uploadFileHandler)
	//http.HandleFunc("/download/", downloadFileHandler)
	fmt.Println("| hfs: download:", downfolderPath, ", upload:", upfolderPath)
	fs := http.FileServer(http.Dir(downfolderPath))
	http.Handle("/download/", http.StripPrefix("/download", fs))

	log.Print("Server Started Listen Port:8080, use /upload/ for uploading files and /download/{fileName} for downloading")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func downloadFileHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("downloadFileHandler: url=", r.URL.Path)
	lst := strings.Split(r.URL.Path, "/")

	filename := filepath.Join(getCurrentPath(), r.URL.Path)
	log.Println("downloadFileHandler: download file=", filename)

	file, err := os.Open(filename)
	if err != nil {
		renderError(w, "INVALID_FILE_TYPE\n", http.StatusNotFound)
		log.Printf("downloadFileHandler: File(%s) INVALID_FILE_TYPE\n", filename)
		return
	}

	defer file.Close()

	fileHeader := make([]byte, 512)
	file.Read(fileHeader)

	fileStat, _ := file.Stat()
	w.Header().Set("Content-Disposition", "attachment; filename="+lst[len(lst)-1])
	w.Header().Set("Content-Type", http.DetectContentType(fileHeader))
	w.Header().Set("Content-Length", strconv.FormatInt(fileStat.Size(), 10))
	w.WriteHeader(http.StatusOK)
	file.Seek(0, 0)
	io.Copy(w, file)
}

func uploadFileHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		log.Printf("| hfs::uploadFileHandler: Could not parse multipart form: %v\n", err)
		renderError(w, "CANT_PARSE_FORM\n", http.StatusInternalServerError)
		return
	}

	md5 := r.FormValue("md5Code")
	oldFileName := r.FormValue("oldFileName")
	devSN := r.FormValue("devSn")
	contName := r.FormValue("containerName")
	appName := r.FormValue("appName")
	log.Printf("| hfs::uploadFileHandler: md5=%s, oldFileName=%s, devSN=%s", md5, oldFileName, devSN)
	if len(contName) > 0 {
		log.Printf("，containerName=%s", contName)
	}
	if len(appName) > 0 {
		log.Printf(", appName=%s", appName)
	}
	log.Println()

	// parse and validate file and post parameters
	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		renderError(w, "INVALID_FILE\n", http.StatusBadRequest)
		log.Printf("| hfs: uploadFileHandler: File(%s) INVALID_FILE\n", oldFileName)
		return
	}
	defer file.Close()
	// Get and print out file size
	fileSize := fileHeader.Size
	log.Printf("| hfs::uploadFileHandler: File(%s) size (bytes): %v\n", fileHeader.Filename, fileSize)
	// validate file size
	if fileSize > maxUploadSize {
		renderError(w, "FILE_TOO_BIG\n", http.StatusBadRequest)
		log.Printf("| hfs::uploadFileHandler: File(%s) FILE_TOO_BIG\n", fileHeader.Filename)
		return
	}
	fileBytes, err := ioutil.ReadAll(file)
	if err != nil {
		renderError(w, "READ INVALID_FILE\n", http.StatusBadRequest)
		log.Printf("| hfs::uploadFileHandler: File(%s) READ INVALID_FILE\n", fileHeader.Filename)
		return
	}

	// check file type, detectcontenttype only needs the first 512 bytes
	detectedFileType := http.DetectContentType(fileBytes)
	switch detectedFileType {
	case "image/jpeg", "image/jpg":
	case "image/gif", "image/png":
	case "application/pdf":
	case "application/octet-stream":
	case "text/plain; charset=utf-8":
		break

	default:
		renderError(w, "INVALID_FILE_TYPE\n", http.StatusBadRequest)
		log.Printf("| hfs::uploadFileHandler: File(%s) INVALID_FILE_TYPE\n", fileHeader.Filename)
		return
	}

	fileName := randToken(12)
	fileEndings, err := mime.ExtensionsByType(detectedFileType)
	if err != nil {
		renderError(w, "CANT_READ_FILE_TYPE\n", http.StatusInternalServerError)
		log.Printf("| hfs::uploadFileHandler: File(%s) CANT_READ_FILE_TYPE\n", fileHeader.Filename)
		return
	}

	_ = fileName
	_ = fileEndings

	upFilePath := filepath.Join(getCurrentPath(), uploadPath)
	upFilePath = filepath.Join(upFilePath, devSN)
	if len(contName) > 0 {
		upFilePath = filepath.Join(upFilePath, contName)
	}
	newPath := filepath.Join(upFilePath, oldFileName)
	log.Printf("| hfs::uploadFileHandler: FileType: %s, File: %s\n", detectedFileType, newPath)

	// write file
	err = os.MkdirAll(upFilePath, os.ModePerm)
	if err != nil {
		renderError(w, "CANT_CREATE_FOLDER\n", http.StatusInternalServerError)
		log.Printf("| hfs::uploadFileHandler: File(%s) CANT_CREATE_FOLDER\n", fileHeader.Filename)
		return
	}
	newFile, err := os.Create(newPath)
	if err != nil {
		renderError(w, "CANT_WRITE_FILE\n", http.StatusInternalServerError)
		log.Printf("| hfs::uploadFileHandler: File(%s) CANT_WRITE_FILE\n", fileHeader.Filename)
		return
	}
	defer newFile.Close() // idempotent, okay to call twice
	if _, err := newFile.Write(fileBytes); err != nil || newFile.Close() != nil {
		renderError(w, "CANT_WRITE_FILE\n", http.StatusInternalServerError)
		log.Printf("| hfs::uploadFileHandler: File(%s) CANT_WRITE_FILE\n", fileHeader.Filename)
		return
	}

	renderError(w, "SUCCESS\n", http.StatusOK)
}

func renderError(w http.ResponseWriter, message string, statusCode int) {
	w.WriteHeader(statusCode)
	w.Write([]byte(message))
}

func randToken(len int) string {
	b := make([]byte, len)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func getCurrentPath() string {
	file, err := exec.LookPath(os.Args[0])
	if err != nil {
		return string("")
	}
	path, err := filepath.Abs(file)
	if err != nil {
		return string("")
	}
	i := strings.LastIndex(path, "/")
	if i < 0 {
		i = strings.LastIndex(path, "\\")
	}
	if i < 0 {
		return string("")
	}
	return string(path[0 : i+1])
}

//
//go build -ldflags "-w -s" -buildmode=c-shared -o hfs.dll HttpFileServer.go
//
