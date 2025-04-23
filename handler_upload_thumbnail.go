package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
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

	// TODO: implement the upload here

	const maxMemory = 10 << 20

	r.ParseMultipartForm(maxMemory)

	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}

	defer file.Close()

	mediaType := header.Header.Get("Content-Type")
	mimeTypes, err := mime.ExtensionsByType(mediaType)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse mime type", err)
	}
	mimeType := mimeTypes[0]

	dbVideo, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to find video", err)
		return
	}
	if dbVideo.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized user", nil)
		return
	}
	randName := make([]byte, 32)
	rand.Read(randName)
	randFileName := base64.RawURLEncoding.EncodeToString(randName)
	filename := fmt.Sprintf("%s%s", randFileName, mimeType)
	videoPath := filepath.Join(cfg.assetsRoot, filename)
	fileOut, err := os.Create(videoPath)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to write file", err)
		return
	}
	defer fileOut.Close()

	_, err = io.Copy(fileOut, file)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to write file", err)
		return
	}

	var newUrl string = fmt.Sprintf("http://localhost:8091/assets/%s", filename)
	dbVideo.ThumbnailURL = &newUrl

	err = cfg.db.UpdateVideo(dbVideo)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Video update failed", err)
		return
	}

	respondWithJSON(w, http.StatusOK, dbVideo)
}
