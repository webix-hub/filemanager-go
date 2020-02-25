package main

import (
	"github.com/dhowden/tag"
	"github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/tiff"
	"net/http"
	"strconv"
)

type MusicMeta struct {
	Title  string
	Artist string
	Album  string
	Year   string
	Genre  string
}

func getMetaInfo(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		panic("id not provided")
	}

	info, err := drive.Info(id)
	if err != nil {
		format.JSON(w, 500, Response{Invalid: true, Error: "Access denied"})
		return
	}

	var meta interface{}
	if info.Type == "audio" {
		meta, err = getMusicMetaInfo(id)
	} else if info.Type == "image" {
		meta, err = getImageMetaInfo(id)
	} else {
		meta = nil
	}

	if err != nil {
		format.JSON(w, 500, Response{Invalid: true, Error: err.Error()})
	} else {
		format.JSON(w, 200, meta)
	}
}

func getMusicMetaInfo(id string) (MusicMeta, error) {
	content, err := drive.Read(id)
	if err != nil {
		return MusicMeta{}, err
	}

	md, err := tag.ReadFrom(content)
	if err != nil {
		return MusicMeta{}, err
	}

	return MusicMeta{
		Title:  md.Title(),
		Artist: md.Artist(),
		Album:  md.Album(),
		Year:   strconv.Itoa(md.Year()),
		Genre:  md.Genre(),
	}, nil
}

func getImageMetaInfo(id string) (map[exif.FieldName]string, error) {
	data, err := drive.Read(id)
	if err != nil {
		return nil, err
	}

	exifmap := make(map[exif.FieldName]string)
	x, err := exif.Decode(data)
	if err == nil || !exif.IsCriticalError(err) {
		x.Walk(walkFunc(func(name exif.FieldName, tag *tiff.Tag) error {
			exifmap[name] = tag.String()
			return nil
		}))
	}

	return exifmap, nil
}
