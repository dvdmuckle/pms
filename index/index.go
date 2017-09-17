package index

import (
	"bufio"
	"os"
	"path"
	"time"

	"github.com/ambientsound/pms/console"
	index_song "github.com/ambientsound/pms/index/song"
	"github.com/ambientsound/pms/song"
	"github.com/ambientsound/pms/xdg"

	"github.com/blevesearch/bleve"

	"fmt"
	"strconv"
)

const INDEX_BATCH_SIZE int = 1000

const SEARCH_SCORE_THRESHOLD float64 = 0.5

type Index struct {
	bleveIndex bleve.Index
	path       string
	indexPath  string
	statePath  string
	version    int
}

func createDirectory(dir string) error {
	dirMode := os.ModeDir | 0755
	return os.MkdirAll(dir, dirMode)
}

// New opens a Bleve index and returns Index. In case an index is not found at
// the given path, a new one is created. In case of an error, nil is returned,
// and the error object set accordingly.
func New(basePath string) (*Index, error) {
	var err error

	timer := time.Now()

	err = createDirectory(basePath)
	if err != nil {
		return nil, fmt.Errorf("while creating %s: %s", basePath, err)
	}

	i := &Index{}
	i.path = basePath
	i.indexPath = path.Join(i.path, "index")
	i.statePath = path.Join(i.path, "state")

	// Try to stat the Bleve index path. If it does not exist, create it.
	if _, err := os.Stat(i.indexPath); err != nil {
		if os.IsNotExist(err) {
			i.bleveIndex, err = create(i.indexPath)
			if err != nil {
				return nil, fmt.Errorf("while creating index at %s: %s", i.indexPath, err)
			}

			// After successful creation, reset the MPD library version.
			err = i.SetVersion(0)
			if err != nil {
				return nil, fmt.Errorf("while zeroing out library version at %s: %s", i.statePath, err)
			}

		} else {
			// In case of any other filesystem error, abort operation.
			return nil, fmt.Errorf("while accessing %s: %s", i.indexPath, err)
		}

	} else {

		// If index was statted ok, try to open it.
		i.bleveIndex, err = open(i.indexPath)
		if err != nil {
			return nil, fmt.Errorf("while opening index at %s: %s", i.indexPath, err)
		}
		i.version, err = i.readVersion()
		if err != nil {
			console.Log("index state file is broken: %s", err)
		}
	}

	console.Log("Opened search index in %s", time.Since(timer).String())

	return i, nil
}

// Close closes a Bleve index.
func (i *Index) Close() error {
	return i.bleveIndex.Close()
}

// create creates a Bleve index at the given file system location.
func create(path string) (bleve.Index, error) {
	mapping, err := buildIndexMapping()
	if err != nil {
		return nil, fmt.Errorf("BUG: unable to create search index mapping: %s", err)
	}

	index, err := bleve.New(path, mapping)
	if err != nil {
		return nil, fmt.Errorf("while creating search index %s: %s", path, err)
	}

	return index, nil
}

// open opens a Bleve index at the given file system location.
func open(path string) (bleve.Index, error) {
	index, err := bleve.Open(path)
	if err != nil {
		return nil, fmt.Errorf("while opening search index %s: %s", path, err)
	}

	return index, nil
}

// Path returns the absolute path to where indexes and state for a specific MPD
// server should be stored.
func Path(host, port string) string {
	cacheDir := xdg.CacheDirectory()
	return path.Join(cacheDir, host, port)
}

// SetVersion writes the MPD library version to the state file.
func (i *Index) SetVersion(version int) error {
	file, err := os.Create(i.statePath)
	if err != nil {
		return err
	}
	defer file.Close()
	str := fmt.Sprintf("%d\n", version)
	file.WriteString(str)
	i.version = version
	return nil
}

// readVersion reads the MPD library version from the state file.
func (i *Index) readVersion() (int, error) {
	file, err := os.Open(i.statePath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		version, err := strconv.Atoi(scanner.Text())
		if err != nil {
			return 0, err
		}
		return version, nil
	}

	return 0, fmt.Errorf("No data in index mpd library state file")
}

func (i *Index) Version() int {
	return i.version
}

// Index the entire Songlist.
func (i *Index) IndexFull(songs []*song.Song) error {
	var err error

	// All operations are batched, currently INDEX_BATCH_SIZE are committed each iteration.
	b := i.bleveIndex.NewBatch()

	for pos, s := range songs {
		is := index_song.New(s)
		err = b.Index(strconv.Itoa(pos), is)
		if err != nil {
			return err
		}
		if pos%INDEX_BATCH_SIZE == 0 {
			console.Log("Indexing songs %d/%d...", pos, len(songs))
			i.bleveIndex.Batch(b)
			b.Reset()
		}
	}
	console.Log("Indexing last batch...")
	i.bleveIndex.Batch(b)

	console.Log("Finished indexing.")

	return nil
}

// Search takes a natural language query string, matches it against the search
// index, and returns a new Songlist with all matching songs.
func (i *Index) Search(q string, size int) ([]int, error) {
	query := bleve.NewQueryStringQuery(q)
	request := bleve.NewSearchRequest(query)
	request.Size = size

	r, _, err := i.Query(request)

	return r, err
}

//// Isolate takes a songlist and a set of tag keys, and matches the tag values
//// of the songlist against the search index.
//func (i *Index) Isolate(list songlist.Songlist, tags []string) (songlist.Songlist, error) {
//terms := make(map[string]struct{})
//query := bleve.NewBooleanQuery()
//songs := list.Songs()

//// Create a cartesian join for song values and tag list.
//for _, song := range songs {
//subQuery := bleve.NewConjunctionQuery()

//for _, tag := range tags {

//// Ignore empty values
//tagValue := song.StringTags[tag]
//if len(tagValue) == 0 {
//continue
//}

//// Name generation
//terms[tagValue] = struct{}{}

//field := strings.Title(tag)
//query := bleve.NewMatchPhraseQuery(tagValue)
//query.SetField(field)
//subQuery.AddQuery(query)
//}
//query.AddShould(subQuery)
//}

//request := bleve.NewSearchRequest(query)
//r, _, err := i.Query(request)

//names := make([]string, 0)
//for k := range terms {
//names = append(names, k)
//}
//name := strings.Join(names, ", ")
//r.SetName(name)

//return r, err
//}

// Query takes a Bleve search request and returns a songlist with all matching songs.
func (i *Index) Query(request *bleve.SearchRequest) ([]int, *bleve.SearchResult, error) {
	//request.Size = 1000

	sr, err := i.bleveIndex.Search(request)

	if err != nil {
		return make([]int, 0), nil, err
	}

	r := make([]int, 0, len(sr.Hits))

	for _, hit := range sr.Hits {
		if hit.Score < SEARCH_SCORE_THRESHOLD {
			break
		}
		id, err := strconv.Atoi(hit.ID)
		if err != nil {
			return r, nil, fmt.Errorf("Index is corrupt; error when converting index IDs to integer: %s", err)
		}
		r = append(r, id)
	}

	console.Log("Query '%s' returned %d results over threshold of %.2f (total %d results) in %s", request, len(r), SEARCH_SCORE_THRESHOLD, sr.Total, sr.Took)

	return r, sr, nil
}
