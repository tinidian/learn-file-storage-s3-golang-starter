package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	const maxBytes = 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	dbVideo, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to find video", err)
		return
	}
	if dbVideo.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized user", nil)
		return
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}

	defer file.Close()

	mimeType := header.Header.Get("Content-Type")

	mediaType, _, err := mime.ParseMediaType(mimeType)
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type", err)
		return
	}

	tempFile, err := os.CreateTemp(cfg.assetsRoot, "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to upload file", err)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	_, err = io.Copy(tempFile, file)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to upload file", err)
		return
	}

	aspectRatio, err := getVideoAspectRatio(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "unable to upload file", err)
		return
	}
	var foldername string
	switch aspectRatio {
	case "16:9":
		foldername = "landscape"
	case "9:16":
		foldername = "portrait"
	default:
		foldername = "other"
	}

	// insert call to fastStart here

	faststartFilepath, err := processVideoForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "unable to upload file", err)
		return
	}

	tempFile.Seek(0, io.SeekStart)

	faststartFile, err := os.Open(faststartFilepath)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "unable to upload file", err)
		return
	}

	defer os.Remove(faststartFile.Name())
	defer faststartFile.Close()

	randFileName := getRandFilename()

	videoKey := fmt.Sprintf("%s/%s", foldername, randFileName)

	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(videoKey),
		Body:        faststartFile,
		ContentType: &mediaType,
	})
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to uplaod file", err)
		return
	}

	var newUrl string = fmt.Sprintf("%s/%s/%s", cfg.s3Distribution, foldername, randFileName)

	dbVideo.VideoURL = &newUrl

	err = cfg.db.UpdateVideo(dbVideo)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Video update failed", err)
		return
	}

	respondWithJSON(w, http.StatusOK, dbVideo)
}

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-select_streams", "v:0", "-show_entries", "stream=display_aspect_ratio", filePath)
	var output bytes.Buffer
	cmd.Stdout = &output
	err := cmd.Run()
	if err != nil {
		log.Printf("Error calling ffprobe: %v", err)
		return "", err
	}
	type probe struct {
		Streams []struct {
			Display_aspect_ratio string `json:"display_aspect_ratio"`
		} `json:"streams"`
	}

	var objmap probe

	err = json.Unmarshal(output.Bytes(), &objmap)
	if err != nil {
		log.Printf("Error unmarshaling: %v", err)
		return "", err
	}

	return objmap.Streams[0].Display_aspect_ratio, nil
}

func processVideoForFastStart(filePath string) (string, error) {
	outputFilepath := filePath + ".processing"
	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputFilepath)
	var output bytes.Buffer
	cmd.Stdout = &output
	err := cmd.Run()
	if err != nil {
		log.Printf("Error calling ffprobe: %v", err)
		return "", err
	}
	return outputFilepath, nil
}

func getRandFilename() string {
	randName := make([]byte, 32)
	rand.Read(randName)
	return base64.RawURLEncoding.EncodeToString(randName)
}
