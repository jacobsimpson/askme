package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"os/exec"
	"os/user"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	md "github.com/MichaelMure/go-term-markdown"
)

type record struct {
	filename string
	tags     []string
	n, EF, I float64
	next     time.Time
}

func (r *record) Name() string {
	return r.filename[0:5]
}

func (r *record) String() string {
	return fmt.Sprintf("{filename: %q, tags: %v, next: %s, supermemo: (%f, %f, %f)}", r.filename, r.tags, r.next, r.n, r.EF, r.I)
}

func (r *record) hasTags(tags []string) bool {
	for _, t := range tags {
		if !contains(r.tags, t) {
			return false
		}
	}
	return true
}

func contains(a []string, v string) bool {
	for _, m := range a {
		if m == v {
			return true
		}
	}
	return false
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

	if len(os.Args) > 1 && os.Args[1] == "add" {
		if err := add(dataDir); err != nil {
			fmt.Fprintf(os.Stderr, "Unable to edit new question: %+v\n", err)
			os.Exit(1)
		}
		return
	}

	ask(dataDir)
}

var questionFile = regexp.MustCompile("[0-9][0-9][0-9][0-9][0-9]-q.md")

func add(dataDir string) error {
	files, err := ioutil.ReadDir(dataDir)
	if err != nil {
		return fmt.Errorf("unable to list the files in %q: %+v", dataDir, err)
	}
	max := 0
	for _, f := range files {
		if questionFile.MatchString(f.Name()) {
			fmt.Printf("Matched = %q\n", f.Name())
			n, err := strconv.Atoi(f.Name()[0:5])
			if err != nil {
				return fmt.Errorf("unable to parse an integer from %q: %+v", f.Name(), err)
			}
			if n > max {
				max = n
			}
		}
	}
	editor := "/home/jacobsimpson/bin/nvim"
	newQuestionFile := path.Join(dataDir, fmt.Sprintf("%05d-q.md", max+1))
	newAnswerFile := path.Join(dataDir, fmt.Sprintf("%05d-a.md", max+1))
	cmd := exec.Command(editor, "-o", newQuestionFile, newAnswerFile)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("unable to start editor %q %q: %+v", editor, newQuestionFile, err)
	}

	records, err := loadIndex(path.Join(dataDir, "index.csv"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}

	records = append(records, &record{
		filename: fmt.Sprintf("%05d-q.md", max+1),
	})

	if err := saveIndex(path.Join(dataDir, "index.csv"), records); err != nil {
		fmt.Fprintf(os.Stderr, "Unable to save the index updates: %+v\n", err)
		os.Exit(1)
	}

	return nil
}

func ask(dataDir string) {
	records, err := loadIndex(path.Join(dataDir, "index.csv"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}

	tags := []string{}
	if len(os.Args) > 1 {
		for _, t := range os.Args[1:] {
			tags = append(tags, t)
		}
	}

	filtered := records
	if len(tags) > 0 {
		filtered = []*record{}
		if err := readTags(dataDir, records); err != nil {
			fmt.Fprintf(os.Stderr, "Unable to load tags: %+v\n", err)
			os.Exit(1)
		}
		for _, r := range records {
			if r.hasTags(tags) {
				filtered = append(filtered, r)
			}
		}
	}

	if len(filtered) == 0 {
		fmt.Printf("No questions to study.\n")
		os.Exit(0)
	}

	sort.Slice(filtered, func(a, b int) bool { return filtered[a].next.After(filtered[b].next) })

	//rand.Seed(time.Now().UnixNano())
	var selected *record //filtered[rand.Intn(len(filtered))]
	for _, f := range filtered {
		if f.next.Before(time.Now()) {
			selected = f
			if !selected.next.IsZero() {
				break
			}
		}
	}

	if selected == nil {
		fmt.Printf("No questions to study.\n")
		os.Exit(0)
	}

	fmt.Printf("%s Asking %s %s\n", strings.Repeat("*", 30), selected.Name(), strings.Repeat("*", 30))
	questionFile := path.Join(dataDir, selected.filename)
	text, err := ioutil.ReadFile(questionFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to read %q\n", questionFile)
		os.Exit(1)
	}

	result := md.Render(string(text), 100, 6)
	fmt.Println(string(result))

	for {
		fmt.Print("Enter your rating (1-5, 1 is hard, 5 is easy): ")
		s, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		s = strings.TrimSpace(s)
		//fmt.Print("rating = " + s)
		if rating, err := strconv.Atoi(s); err == nil {
			updateRating(selected, rating)
			break
		}
		fmt.Printf("%q is not a valid choice\n", s)
	}

	if err := saveIndex(path.Join(dataDir, "index.csv"), records); err != nil {
		fmt.Fprintf(os.Stderr, "Unable to save the index updates: %+v\n", err)
		os.Exit(1)
	}
}

func updateRating(r *record, q int) {
	//algorithm SM-2 is
	//    input:  user grade q
	//            repetition number n
	//            easiness factor EF
	//            interval I
	//    output: updated values of n, EF, and I
	//
	//    if q ≥ 3 (correct response) then
	//        if n = 0 then
	//            I ← 1
	//        else if n = 1 then
	//            I ← 6
	//        else
	//            I ← ⌈I × EF⌉
	//        end if
	//        EF ← EF + (0.1 − (5 − q) × (0.08 + (5 − q) × 0.02)
	//        if EF < 1.3 then
	//            EF ← 1.3
	//        end if
	//        increment n
	//    else (incorrect response)
	//        n ← 0
	//        I ← 1
	//    end if
	//
	//    return (n, EF, I)

	if q >= 3 {
		if r.n == 0 {
			r.I = 1
		} else if r.n == 1 {
			r.I = 6
		} else {
			r.I = math.Ceil(r.I * r.EF)
		}
		r.EF = r.EF * (0.1 - (5.0-float64(q))*(0.08+(5-float64(q))*0.02))
		if r.EF < 1.3 {
			r.EF = 1.3
		}
		r.n++
	} else {
		r.n = 0
		r.I = 1
	}
	r.next = time.Now().Add(time.Duration(r.I) * time.Hour)
	fmt.Printf("updated record = %s\n", r)
}

func readTags(dataDir string, records []*record) error {
	for _, r := range records {
		text, err := ioutil.ReadFile(path.Join(dataDir, r.filename))
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(text), "\n") {
			if strings.HasPrefix(line, "Tags: ") {
				for _, s := range strings.Split(line[6:], ",") {
					r.tags = append(r.tags, strings.TrimSpace(s))
				}
			}
		}
	}
	return nil
}

func loadIndex(indexFile string) ([]*record, error) {
	f, err := os.Open(indexFile)
	if os.IsNotExist(err) {
		return []*record{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("unable to open the index file %q: %+v", indexFile, err)
	}
	r := csv.NewReader(f)

	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("unable to read index file %q: %+v", indexFile, err)
	}

	result := []*record{}
	for _, r := range records {
		n, _ := strconv.ParseFloat(r[1], 32)
		EF, _ := strconv.ParseFloat(r[2], 32)
		I, _ := strconv.ParseFloat(r[3], 32)
		t, _ := time.Parse(time.RFC3339, r[4])
		result = append(result, &record{
			filename: r[0],
			n:        n,
			EF:       EF,
			I:        I,
			next:     t,
		})
	}
	return result, nil
}

func saveIndex(indexFile string, records []*record) error {
	newIndexFile := fmt.Sprintf("%s.new", indexFile)
	oldIndexFile := fmt.Sprintf("%s.old", indexFile)

	newIndex, err := os.Create(newIndexFile)
	if err != nil {
		return fmt.Errorf("unable to create new index file %q: %+v", newIndexFile, err)
	}
	defer newIndex.Close()

	writer := csv.NewWriter(newIndex)
	defer writer.Flush()

	for _, record := range records {
		err := writer.Write([]string{
			record.filename,
			fmt.Sprintf("%f", record.n),
			fmt.Sprintf("%f", record.EF),
			fmt.Sprintf("%f", record.I),
			record.next.Format(time.RFC3339),
		})
		if err != nil {
			return fmt.Errorf("unable to write record %s to index: %+v", record, err)
		}
	}

	if err := os.Remove(oldIndexFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("unable to remove the old index file %q: %+v", oldIndexFile, err)
	}
	if err := os.Rename(indexFile, oldIndexFile); err != nil {
		return fmt.Errorf("unable to rename the current index file %q to %q: %+v", indexFile, oldIndexFile, err)
	}
	if err := os.Rename(newIndexFile, indexFile); err != nil {
		return fmt.Errorf("unable to rename the new index file %q to %q: %+v", newIndexFile, indexFile, err)
	}
	return nil
}
