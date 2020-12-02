//usr/bin/env go run "$0" "$@" ; exit "$?"
package main

import (
	"encoding/csv"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/user"
	"path"
	"time"

	md "github.com/MichaelMure/go-term-markdown"
)

type record struct {
	filename string
}

func (r *record) String() string {
	return r.filename
}

func main() {
	usr, err := user.Current()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to determine the current user.\n")
		os.Exit(1)
	}

	dataDir := path.Join(usr.HomeDir, ".askme")
	if err := os.Mkdir(dataDir, os.ModeDir|os.ModePerm); err != nil && !os.IsExist(err) {
		fmt.Fprintf(os.Stderr, "Unable to create the askme data directory %q: %+v\n", dataDir, err)
		os.Exit(1)
	}

	records, err := loadIndex(path.Join(dataDir, "index.csv"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}

	rand.Seed(time.Now().UnixNano())
	selected := records[rand.Intn(len(records))]
	fmt.Printf("Asking %q\n", selected)
	questionFile := path.Join(dataDir, selected.filename)
	text, err := ioutil.ReadFile(questionFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to read %q\n", questionFile)
		os.Exit(1)
	}

	result := md.Render(string(text), 100, 6)
	fmt.Println(string(result))
}

func loadIndex(indexFile string) ([]*record, error) {
	f, err := os.Open(indexFile)
	if os.IsNotExist(err) {
		return []*record{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("Unable to open the index file %q: %+v", indexFile, err)
	}
	r := csv.NewReader(f)

	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("Unable to read index file %q: %+v", indexFile, err)
	}

	result := []*record{}
	for _, r := range records {
		result = append(result, &record{filename: r[0]})
	}
	return result, nil
}
