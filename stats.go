package main

import (
	"net/http"
)

type FSStats struct {
	Free  uint64 `json:"free"`
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
}

type FSInfo struct {
	Stats    FSStats    `json:"stats"`
	Features FSFeatures `json:"features"`
}

func getInfo(w http.ResponseWriter, r *http.Request) {
	used, free, err := drive.Stats()
	if err != nil {
		format.JSON(w, 500, Response{Invalid: true, Error: err.Error()})
		return
	}

	total := free + used

	format.JSON(w, 200, FSInfo{
		Stats:    FSStats{Free: free, Used: used, Total: total},
		Features: features,
	})
}
