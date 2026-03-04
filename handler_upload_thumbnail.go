package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

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

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	// TODO: implement the upload here

	const maxMemory = 10 << 20

	err = r.ParseMultipartForm(maxMemory)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "ParseMultipartForm error", err)
		return
	}

	file, header, err := r.FormFile("thumbnail") // header thing unused
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "FormFile error", err)
		return
	}

	contentTypeHeader := header.Header.Get("Content-Type")
	if contentTypeHeader == "" {
		respondWithError(w, http.StatusBadRequest, "No Content-Type Header", nil)
		return
	}

	fileData, err := io.ReadAll(file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "ReadAll error", err)
		return
	}

	videoMetadata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not fetch video metadata", err)
		return
	}

	log.Print(videoMetadata) // debug

	if videoMetadata.UserID != userID {
		log.Printf("\n%s\n%s", videoMetadata.UserID.String(), userID.String())
		respondWithError(w, http.StatusUnauthorized, "Not the video author", err)
		return
	}

	videoThumbnails[videoMetadata.ID] = thumbnail{
		data:      fileData,
		mediaType: contentTypeHeader, // seems about right?
	}

	port := os.Getenv("PORT")
	if port == "" {
		log.Fatal("PORT environment variable is not set")
	}

	thumbnailURL := fmt.Sprintf("http://localhost:%s/api/thumbnails/%s", port, videoMetadata.ID)
	videoMetadata.ThumbnailURL = &thumbnailURL

	err = cfg.db.UpdateVideo(videoMetadata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video metadata", err)
		return
	}

	// /TODO

	respondWithJSON(w, http.StatusOK, videoMetadata)
}
