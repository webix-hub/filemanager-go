package main

import (
	"flag"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/cors"
	"github.com/jinzhu/configor"
	"github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/tiff"
	"github.com/unrolled/render"

	"github.com/xbsoftware/wfs"
	local "github.com/xbsoftware/wfs-local"
)

var format = render.New()

type Response struct {
	Invalid bool   `json:"invalid"`
	Error   string `json:"error"`
	ID      string `json:"id"`
}

type FSFeatures struct {
	Preview map[string]bool `json:"preview"`
	Meta    map[string]bool `json:"meta"`
}

var drive wfs.Drive
var features = FSFeatures{
	Preview: map[string]bool{},
	Meta:    map[string]bool{},
}

type AppConfig struct {
	DataFolder  string
	Port        string
	Preview     string
	UploadLimit int64
	Readonly    bool
}

var Config AppConfig

func main() {
	flag.StringVar(&Config.Preview, "preview", "", "url of preview generation service")
	flag.BoolVar(&Config.Readonly, "readonly", false, "readonly mode")
	flag.Int64Var(&Config.UploadLimit, "limit", 10_000_000, "max file size to upload")
	flag.StringVar(&Config.Port, "port", ":3200", "port for web server")
	flag.Parse()

	args := flag.Args()
	if len(args) > 0 {
		Config.DataFolder = args[0]
	}

	configor.New(&configor.Config{ENVPrefix: "APP", Silent: true}).Load(&Config, "config.yml")

	// configure features
	features.Meta["audio"] = true
	features.Meta["image"] = true
	features.Preview["image"] = true
	if Config.Preview != "" {
		features.Preview["document"] = true
		features.Preview["code"] = true
	}

	// common drive access
	var err error
	driveConfig := wfs.DriveConfig{Verbose: true}
	driveConfig.Operation = &wfs.OperationConfig{PreventNameCollision: true}
	if Config.Readonly {
		temp := wfs.Policy(&wfs.ReadOnlyPolicy{})
		driveConfig.Policy = &temp
	}

	drive, err = local.NewLocalDrive(Config.DataFolder, &driveConfig)
	if err != nil {
		log.Fatal(err)
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	cors := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		AllowCredentials: true,
		MaxAge:           300,
	})
	r.Use(cors.Handler)

	r.Get("/icons/{size}/{type}/{name}", func(w http.ResponseWriter, r *http.Request) {
		size := chi.URLParam(r, "size")
		name := chi.URLParam(r, "name")
		ftype := chi.URLParam(r, "type")

		http.ServeFile(w, r, getIconURL(size, ftype, name))
	})

	r.Get("/preview", getFilePreview)

	r.Get("/search", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		search := r.URL.Query().Get("search")

		data, err := drive.Search(id, search)

		if err != nil {
			format.Text(w, 500, err.Error())
			return
		}
		format.JSON(w, 200, data)
	})

	r.Get("/files", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			id = "/"
		}

		search := r.URL.Query().Get("search")

		var config *wfs.ListConfig
		if search == "" {
			config = &wfs.ListConfig{
				Nested:  true,
				Exclude: func(name string) bool { return strings.HasPrefix(name, ".") },
			}
		} else {
			search = strings.ToLower(search)
			config = &wfs.ListConfig{
				SubFolders: true,
				Include:    func(name string) bool { return strings.Contains(strings.ToLower(name), search) },
				Exclude:    func(name string) bool { return strings.HasPrefix(name, ".") },
			}
		}

		data, err := drive.List(id, config)

		if err != nil {
			format.Text(w, 500, err.Error())
			return
		}

		err = format.JSON(w, 200, data)
	})

	r.Get("/folders", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			id = "/"
		}

		data, err := drive.List(id, &wfs.ListConfig{
			Nested:     true,
			SubFolders: true,
			SkipFiles:  true,
			Exclude:    func(name string) bool { return strings.HasPrefix(name, ".") },
		})

		if err != nil {
			format.Text(w, 500, err.Error())
			return
		}

		err = format.JSON(w, 200, data)
	})

	r.Post("/copy", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		id := r.Form.Get("id")
		to := r.Form.Get("to")
		if id == "" || to == "" {
			panic("both, 'id' and 'to' parameters must be provided")
		}

		id, err := drive.Copy(id, to, "")
		if err != nil {
			format.JSON(w, 500, Response{Invalid: true, Error: err.Error()})
			return
		}

		info, err := drive.Info(id)
		if err != nil {
			format.JSON(w, 500, Response{Invalid: true, Error: err.Error()})
			return
		}

		format.JSON(w, 200, info)
	})

	r.Post("/move", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		id := r.Form.Get("id")
		to := r.Form.Get("to")
		if id == "" || to == "" {
			panic("both, 'id' and 'to' parameters must be provided")
		}

		id, err := drive.Move(id, to, "")
		if err != nil {
			format.JSON(w, 500, Response{Invalid: true, Error: err.Error()})
			return
		}

		info, err := drive.Info(id)
		if err != nil {
			format.JSON(w, 500, Response{Invalid: true, Error: err.Error()})
			return
		}

		format.JSON(w, 200, info)
	})

	r.Post("/rename", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		id := r.Form.Get("id")
		name := r.Form.Get("name")
		if id == "" || name == "" {
			panic("both, 'id' and 'name' parameters must be provided")
		}

		id, err := drive.Move(id, "", name)
		if err != nil {
			format.JSON(w, 500, Response{Invalid: true, Error: err.Error()})
			return
		}

		format.JSON(w, 200, Response{ID: id})
	})

	r.Post("/upload", func(w http.ResponseWriter, r *http.Request) {
		// buffer for file parsing, this is NOT the max upload size
		var limit = int64(32 << 20) // default is 32MB
		if Config.UploadLimit < limit {
			limit = Config.UploadLimit
		}

		// this one limit max upload size
		r.Body = http.MaxBytesReader(w, r.Body, Config.UploadLimit)
		r.ParseMultipartForm(limit)

		file, handler, err := r.FormFile("upload")
		if err != nil {
			panic("Error Retrieving the File")
		}
		defer file.Close()

		base := r.URL.Query().Get("id")

		parts := strings.Split(handler.Filename, "/")
		if len(parts) > 1 {
			for _, p := range parts[:len(parts)-1] {
				if !drive.Exists(base + "/" + p) {
					id, err := drive.Make(base, p, true)
					if err != nil {
						format.JSON(w, 500, Response{Invalid: true, Error: err.Error()})
						return
					}
					base = id
				} else {
					base = base + "/" + p
				}
			}
		}

		fileID, err := drive.Make(base, parts[len(parts)-1], false)
		if err != nil {
			format.Text(w, 500, "Access Denied")
			return
		}

		err = drive.Write(fileID, file)
		if err != nil {
			format.Text(w, 500, "Access Denied")
			return
		}

		info, err := drive.Info(fileID)
		format.JSON(w, 200, info)
	})

	r.Post("/makefile", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		id := r.Form.Get("id")
		name := r.Form.Get("name")
		if id == "" || name == "" {
			panic("both, 'id' and 'name' parameters must be provided")
		}

		id, err := drive.Make(id, name, false)
		if err != nil {
			format.JSON(w, 500, Response{Invalid: true, Error: err.Error()})
			return
		}

		info, err := drive.Info(id)
		if err != nil {
			format.JSON(w, 500, Response{Invalid: true, Error: err.Error()})
			return
		}

		format.JSON(w, 200, info)
	})

	r.Post("/makedir", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		id := r.Form.Get("id")
		name := r.Form.Get("name")
		if id == "" || name == "" {
			panic("both, 'id' and 'name' parameters must be provided")
		}

		id, err := drive.Make(id, name, true)
		if err != nil {
			format.JSON(w, 500, Response{Invalid: true, Error: err.Error()})
			return
		}

		info, err := drive.Info(id)
		if err != nil {
			format.JSON(w, 500, Response{Invalid: true, Error: err.Error()})
			return
		}

		format.JSON(w, 200, info)
	})

	r.Post("/delete", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		id := r.Form.Get("id")
		if id == "" {
			panic("path not provided")
		}

		err := drive.Remove(id)
		if err != nil {
			format.JSON(w, 500, Response{Invalid: true, Error: err.Error()})
			return
		}

		format.JSON(w, 200, Response{})
	})

	r.Get("/text", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			panic("id not provided")
		}

		data, err := drive.Read(id)
		if err != nil {
			panic(err)
		}

		w.Header().Add("Content-type", "text/plain")
		io.Copy(w, data)
	})

	r.Post("/text", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()

		id := r.Form.Get("id")
		content := r.Form.Get("content")
		if id == "" {
			panic("id not provided")
		}

		err := drive.Write(id, strings.NewReader(content))
		if err != nil {
			panic(err)
		}

		info, _ := drive.Info(id)

		format.JSON(w, 200, info)
	})

	r.Get("/direct", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			panic("id not provided")
		}

		info, err := drive.Info(id)
		if err != nil {
			format.Text(w, 500, "Access denied")
			return
		}

		data, err := drive.Read(id)
		if err != nil {
			format.Text(w, 500, "Access denied")
			return
		}

		disposition := "inline"
		_, ok := r.URL.Query()["download"]
		if ok {
			disposition = "attachment"
		}

		w.Header().Set("Content-Disposition", disposition+"; filename=\""+info.Name+"\"")
		http.ServeContent(w, r, "", time.Now(), data)
	})

	r.Get("/info", getInfo)
	r.Get("/meta", getMetaInfo)

	log.Printf("Starting webserver at port " + Config.Port)
	http.ListenAndServe(Config.Port, r)
}

type walkFunc func(exif.FieldName, *tiff.Tag) error

func (f walkFunc) Walk(name exif.FieldName, tag *tiff.Tag) error {
	return f(name, tag)
}
