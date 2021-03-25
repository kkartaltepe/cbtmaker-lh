package main

import (
	"archive/tar"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func coalescS(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func justAttr(s string, _ bool) string {
	return s
}

func ripChapter(chapterURL string) (string, string, []*url.URL) {
	resp, err := http.Get(chapterURL)
	if err != nil {
		log.Fatalf("Failed to pull chapter: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Fatalf("Bad response: (%v) %s", resp.StatusCode, resp.Status)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		log.Fatalf("Bad parse: %v", err)
	}

	var pages []*url.URL
	sel := doc.Find("div.chapter-content img.chapter-img")
	for i := range sel.Nodes {
		s := sel.Eq(i)
		img := coalescS(
			justAttr(s.Attr("data-srcset")),
			justAttr(s.Attr("data-aload")), // copy cats use?
			justAttr(s.Attr("src")),        // usually ad
		)
		parsed, err := url.Parse(strings.TrimSpace(img))
		if err != nil || parsed.Scheme != "https" {
			// if its ad, sorry bruh.
			log.Printf("[E] Bad page url(%v): %v", i, err)
			continue
		}
		pages = append(pages, parsed)
	}

	var title, chapter string
	doc.Find("section#chapters").Each(func(_ int, s *goquery.Selection) {
		s.Find("h5 a").Each(func(_ int, s2 *goquery.Selection) {
			title = strings.TrimSpace(s2.Text())
		})
	})

	doc.Find("ul#chap_list li.current a").Each(func(_ int, s *goquery.Selection) {
		chapter = strings.TrimSpace(s.Text())
	})

	return title, chapter, pages
}

func ripChapterNew(chapterURL string) (string, string, []*url.URL) {
	resp, err := http.Get(chapterURL)
	if err != nil {
		log.Fatalf("Failed to pull chapter: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Fatalf("Bad response: (%v) %s", resp.StatusCode, resp.Status)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		log.Fatalf("Bad parse: %v", err)
	}

	var pages []*url.URL
	sel := doc.Find("div.mb-3 img.img-fluid")
	for i := range sel.Nodes {
		s := sel.Eq(i)
		img := coalescS(
			justAttr(s.Attr("data-srcset")),
			justAttr(s.Attr("data-src")),
			justAttr(s.Attr("data-aload")), // copy cats use?
			justAttr(s.Attr("src")),        // usually ad
		)
		parsed, err := url.Parse(strings.TrimSpace(img))
		if err != nil || parsed.Scheme != "https" {
			// if its ad, sorry bruh.
			log.Printf("[E] Bad page url(%v): %v", i, err)
			continue
		}
		pages = append(pages, parsed)
	}

	var title, chapter string
	doc.Find("main#reader-basic div.container").Each(func(_ int, s *goquery.Selection) {
		s.Find("h1").Each(func(_ int, s2 *goquery.Selection) {
			parts := strings.SplitAfterN(strings.TrimSpace(s2.Text()), "|", 2)
			chapter = strings.TrimSpace(parts[0][:len(parts[0])-2])
			title = strings.TrimSpace(parts[1])
		})
	})

	return title, chapter, pages
}

type sizedReadCloser struct {
	R    io.ReadCloser
	Size int64
}

func makeTar(file, title, chapter string, pages []sizedReadCloser) {
	dir := path.Dir(file)
	if dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("Create tar dir(%s) failed: %v", dir, err)
		}
	}
	fhd, err := os.Create(file)
	if err != nil {
		log.Fatalf("Create tar failed: %v", err)
	}
	defer fhd.Close()

	tw := tar.NewWriter(fhd)
	for i, page := range pages {
		path := path.Join(title+" "+chapter, fmt.Sprintf("%03d.jpg", i))
		hdr := &tar.Header{
			Name: path,
			Mode: 0644,
			Size: page.Size,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			log.Fatalf("Tar header error: %v", err)
		}
		if _, err := io.Copy(tw, page.R); err != nil {
			log.Fatalf("Tar write error: %v", err)
		}
		page.R.Close()
	}

	if err := tw.Close(); err != nil {
		log.Fatalf("Tar finish error: %v", err)
	}
}

func getPages(pages []*url.URL, ref string) []sizedReadCloser {
	var data []sizedReadCloser
	client := &http.Client{}
	for _, page := range pages {
		resp, err := client.Do(&http.Request{
			Method: "GET",
			URL:    page,
			Header: http.Header{
				"Referer": {ref},
			},
		})
		if err != nil {
			log.Fatalf("Failed to pull chapter: %v", err)
		}

		if resp.StatusCode != 200 {
			resp.Body.Close()
			log.Fatalf("Bad response: (%v) %s", resp.StatusCode, resp.Status)
		}

		if resp.ContentLength == -1 {
			log.Fatalf("Unknown content length for page: %s", page)
		}
		data = append(data, sizedReadCloser{R: resp.Body, Size: resp.ContentLength})
	}
	return data
}

func getChaptersStartingAt(start string) []string {
	startURL, err := url.Parse(start)
	if err != nil {
		log.Fatalf("Failed to parse URL (%s), getChapters: %v", start, err)
	}
	// normalize for checking later.
	start = startURL.String()
	resp, err := http.Get(start)
	if err != nil {
		log.Fatalf("Failed to pull chapter: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Fatalf("Bad response: (%v) %s", resp.StatusCode, resp.Status)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		log.Fatalf("Bad parse: %v", err)
	}

	startURL.Path = ""
	startURL.RawPath = ""
	startURL.RawQuery = ""
	startURL.ForceQuery = false
	startURL.Fragment = ""
	startURL.RawFragment = ""
	var chapters []string
	doc.Find("ul#chap_list li a").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		startURL.Path = href
		chapters = append(chapters, startURL.String())
	})

	// swap order (chapter list is from high to low)
	for i, j := 0, len(chapters)-1; i < j; i, j = i+1, j-1 {
		chapters[i], chapters[j] = chapters[j], chapters[i]
	}

	// start from start.
	i := 0
	for ; chapters[i] != start && i < len(chapters); i = i + 1 {
	}
	return chapters[i:]
}

func getChaptersStartingAtNew(start string) []string {
	startURL, err := url.Parse(start)
	if err != nil {
		log.Fatalf("Failed to parse URL (%s), getChapters: %v", start, err)
	}
	// normalize for checking later.
	start = startURL.String()
	resp, err := http.Get(start)
	if err != nil {
		log.Fatalf("Failed to pull chapter: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Fatalf("Bad response: (%v) %s", resp.StatusCode, resp.Status)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		log.Fatalf("Bad parse: %v", err)
	}

	startURL.Path = ""
	startURL.RawPath = ""
	startURL.RawQuery = ""
	startURL.ForceQuery = false
	startURL.Fragment = ""
	startURL.RawFragment = ""
	var chapters []string
	doc.Find("div.list-group-item-action a").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		startURL.Path = href
		chapters = append(chapters, startURL.String())
	})

	// swap order (chapter list is from high to low)
	for i, j := 0, len(chapters)-1; i < j; i, j = i+1, j-1 {
		chapters[i], chapters[j] = chapters[j], chapters[i]
	}

	return chapters
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("You must provide the URL to the first chapter")
	}

	chapterURLs := getChaptersStartingAtNew(os.Args[1])
	for _, chapterURL := range chapterURLs {
		title, chapter, pages := ripChapterNew(chapterURL)
		log.Printf("T:%s, C:%s, P#:%v", title, chapter, len(pages))
		pagesData := getPages(pages, chapterURL)
		makeTar(path.Join(title, title+" "+chapter+".cbt"), title, chapter, pagesData)
	}
}
