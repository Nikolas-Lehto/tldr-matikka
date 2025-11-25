package main

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

type App struct {
	MD        goldmark.Markdown
	Templates map[string]*template.Template
	Fragments map[string]*template.Template
	Log       *log.Logger
	Cources   []string
}

// Render markdown template
func (app *App) render(t string, data any) template.HTML {
	var rbuf bytes.Buffer
	// Execute the markdown fragment template from the Fragments map.
	frag, ok := app.Fragments[t]
	if !ok {
		app.Log.Printf("fragment template %q not found", t)
	} else {
		if err := frag.ExecuteTemplate(&rbuf, t, data); err != nil {
			app.Log.Print(err)
		}
	}

	var wbuf bytes.Buffer
	if err := app.MD.Convert(rbuf.Bytes(), &wbuf); err != nil {
		app.Log.Print(err)
	}

	return "<article>\n" + template.HTML(wbuf.String()) + "\n</article>"
}

// loadTemplates recursively parses all templates in the given directory.
func (app *App) loadTemplates(dir string) error {
	app.Templates = make(map[string]*template.Template)
	app.Fragments = make(map[string]*template.Template)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		if _, err := filepath.Rel(dir, path); err != nil {
			return err
		}

		// If file is a markdown fragment (cources), parse as a named template
		// stored in Fragments map.
		if strings.HasSuffix(path, ".md") {
			name := filepath.Base(path)
			// Parse the markdown file as a template with its filename as the
			// template name so it can be executed later. Attach FuncMap so
			// the fragment can call the render function to include other
			// fragments.
			t := template.New(name).Funcs(template.FuncMap{"render": app.render})
			if _, err := t.ParseFiles(path); err != nil {
				return fmt.Errorf("could not parse fragment %s: %w", path, err)
			}
			app.Fragments[name] = t
			return nil
		}

		// For html templates, create a new template that includes base.html
		// and the page, so that block definitions won't collide.
		if strings.HasSuffix(path, ".html") {
			name := filepath.Base(path)
			// Create a template and parse base.html first if available.
			t := template.New(name).Funcs(template.FuncMap{"render": app.render})
			// Parse all html files in the dir so templates can reference
			// base.html. Use ParseFiles with base.html and the specific page.
			// If the file is base.html itself, just parse it.
			if name == "base.html" {
				if _, err := t.ParseFiles(path); err != nil {
					return fmt.Errorf("could not parse template %s: %w", path, err)
				}
				app.Templates[name] = t
				return nil
			}

			// Parse base + page so the page's define blocks override base's
			// blocks without affecting other pages.
			basePath := filepath.Join(dir, "base.html")
			if _, err := os.Stat(basePath); err == nil {
				if _, err := t.ParseFiles(basePath, path); err != nil {
					return fmt.Errorf("could not parse template %s with base: %w", path, err)
				}
			} else {
				if _, err := t.ParseFiles(path); err != nil {
					return fmt.Errorf("could not parse template %s: %w", path, err)
				}
			}
			app.Templates[name] = t
		}

		return nil
	})

	return err
}

// renderIndex handles rendering the index.html template.
func (app *App) renderIndex(w http.ResponseWriter, r *http.Request) {
	t, ok := app.Templates["index.html"]
	if !ok {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		app.Log.Print("index.html template not found")
		return
	}

	err := t.ExecuteTemplate(w, "base.html", map[string]any{
		"CurrentCource": "matikka",
		"Cources":       app.Cources,
	})
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		app.Log.Print(err)
	}
}

func (app *App) renderCource(w http.ResponseWriter, r *http.Request) {
	// Extract the course slug from the URL path. net/http does not provide
	// path parameter parsing by default, so we take the suffix after
	// "/cource/".
	path := r.URL.Path
	cource := strings.TrimPrefix(path, "/cource/")
	cource = strings.Trim(cource, "/")

	if cource == "" {
		http.NotFound(w, r)
		return
	}

	t, ok := app.Templates["cource.html"]
	if !ok {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		app.Log.Print("cource.html template not found")
		return
	}

	err := t.ExecuteTemplate(w, "base.html", map[string]any{
		"CurrentCource":     cource,
		"CurrentCourceSlug": cource + ".md",
		"Cources":           app.Cources,
	})

	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		app.Log.Print(err)
	}
}

func main() {
	app := App{
		Log: log.Default(),
		MD: goldmark.New(
			goldmark.WithExtensions(
				extension.GFM,
				extension.Footnote,
				extension.Typographer,
				MathML(),
			),
			goldmark.WithParserOptions(
				parser.WithAutoHeadingID(),
			),
			goldmark.WithRendererOptions(
				html.WithUnsafe(),
			),
		),
	}

	app.Log.Print("Hello!")

	// List cources
	cources, err := filepath.Glob("content/cources/*.md")
	if err != nil {
		app.Log.Fatal("Failed to list cources: ", err)
	}

	for _, cource := range cources {
		app.Cources = append(app.Cources, strings.TrimPrefix(strings.TrimSuffix(cource, ".md"), "content/cources/"))
	}

	// Initialize template maps and load templates
	app.Templates = make(map[string]*template.Template)
	app.Fragments = make(map[string]*template.Template)

	err = app.loadTemplates("content")
	if err != nil {
		app.Log.Print("Error loading templates:", err)
		return
	}

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))

	http.HandleFunc("/cource/", app.renderCource)
	http.HandleFunc("/{$}", app.renderIndex)

	app.Log.Print("Starting a server on http://localhost:8080")
	app.Log.Fatal(http.ListenAndServe(":8080", nil))
}
