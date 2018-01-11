package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/ioutil"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"golang.org/x/text/language"

	"cloud.google.com/go/translate"
	"github.com/boltdb/bolt"
	"github.com/urfave/cli"
	"google.golang.org/api/option"
)

const EJ_GOOGLE_TRANS_API_KEY_ENV = "EJ_GOOGLE_TRANS_API_KEY"
const (
	BOLT_TRANSLATE_BUCKET = "trans_cache"
	BOLT_DICT_BUCKET      = "dict_cache"
)

func main() {
	// TODO debug
	panic(fmt.Errorf("%#v", fetchDictFromAPI([]string{"love"})))

	app := cli.NewApp()
	app.Name = "ej"
	app.Description = `simple Japanese <->English translator.
	 once translated result will be cached in local database at "$HOME/.ej"`

	app.Usage = "ej [sentense]"
	app.Commands = nil
	app.Version = "0.0.2"
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "l",
			Usage: "list all caches",
		},
		cli.BoolFlag{
			Name:  "f",
			Usage: "force translate. not use cache",
		},
		cli.BoolFlag{
			Name:  "r",
			Usage: "force reverse translation(some language to english) ",
		},
		cli.BoolFlag{
			Name:  "json",
			Usage: "output in json format",
		},
	}

	app.Action = func(c *cli.Context) error {
		src := strings.Join(c.Args(), " ")
		if len(src) == 0 {
			return nil
		}

		var printer func(tr TranslateAndDicts)
		var slicePrinter func(tr []TranslateAndDicts)

		// set printer
		if c.Bool("json") {
			printer = jsonPrinter
			slicePrinter = jsonSlicePrinter
		} else {
			printer = plainPrinter
			slicePrinter = plainSlicePrinter
		}

		cacheDB, err := loadCacheDB()
		if err != nil {
			return err
		}
		defer cacheDB.Close()

		if c.Bool("l") { // list cached
			slicePrinter(fetchCacheList(cacheDB, src))
			return nil
		}

		// search from cache
		if !c.Bool("f") {
			t, found := fetchTranslationFromCache(cacheDB, src)
			if found {
				printer(t)
				return nil
			}
		}

		apiKey := os.Getenv(EJ_GOOGLE_TRANS_API_KEY_ENV)
		if apiKey == "" {
			return fmt.Errorf("need '%s' env variable", EJ_GOOGLE_TRANS_API_KEY_ENV)
		}

		ctx := context.Background()
		client, err := translate.NewClient(ctx, option.WithAPIKey(apiKey))
		if err != nil {
			return err
		}

		// default , somelang to japanese
		destLang := language.Japanese
		input := []string{src}

		if c.Bool("r") {
			// translate to english if force reverse
			destLang = language.English
		} else {
			// translate to english if input is japanese
			detectedInputLangs, err := client.DetectLanguage(ctx, input)
			if err == nil {
				for _, detectedInputLangsArr := range detectedInputLangs {
					for _, detectedInputLang := range detectedInputLangsArr {
						if detectedInputLang.Language == language.Japanese {
							destLang = language.English
							goto FinishDetectLang
						}
					}
				}
			}

		FinishDetectLang:
		}

		resp, err := client.Translate(ctx, input, destLang, nil)
		if err != nil {
			return err
		}

		inputLang := resp[0].Source
		translated := newTranslate(inputLang, src, destLang, resp[0].Text)
		err = putTranslationToCache(cacheDB, translated)
		if err != nil {
			return err
		}

		var dicts []Dict
		if destLang == language.English {
			dicts = fetchDictOfWords(translated.Translated, false)
		} else if inputLang == language.English {
			dicts = fetchDictOfWords(translated.Input, false)
		}

		result := TranslateAndDicts{
			Translate: translated,
			Dicts:     dicts,
		}

		printer(result)
		return nil
	}

	app.Run(os.Args)
}

var noDefinitionAPIKey = errors.New("no definitiion api key")

func loadCacheDB() (*bolt.DB, error) {
	dbDir := expandFilePath("$HOME/.ej")
	if _, err := os.Stat(dbDir); os.IsNotExist(err) {
		err := os.MkdirAll(dbDir, 0755)
		if err != nil {
			return nil, err
		}
	}

	db, err := bolt.Open(filepath.Join(dbDir, "ej.db"), 0755, nil)
	return db, err
}

func fetchCacheList(db *bolt.DB, src string) []TranslateAndDicts {
	var cachedResults []TranslateAndDicts
	db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(BOLT_TRANSLATE_BUCKET))
		bucket.ForEach(func(k, v []byte) error {
			var tr Translate
			if err := json.Unmarshal(v, &tr); err == nil {
				var dicts []Dict
				if tr.IsInputIsEng() {
					dicts = fetchDictOfWords(tr.Input, true)
				} else if tr.IsTranslatedIsEng() {
					dicts = fetchDictOfWords(tr.Translated, true)
				}
				result := TranslateAndDicts{
					Translate: tr,
					Dicts:     dicts,
				}
				cachedResults = append(cachedResults, result)
			}
			return nil
		})

		return nil
	})
	return cachedResults
}

func fetchTranslationFromCache(db *bolt.DB, src string) (TranslateAndDicts, bool) {
	// get from cache if exists
	result := TranslateAndDicts{}
	found := false
	db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(BOLT_TRANSLATE_BUCKET))
		if bucket == nil {
			return nil
		}
		v := bucket.Get([]byte(src))
		if len(v) == 0 {
			return nil
		}

		var tr Translate
		if err := json.Unmarshal(v, &tr); err == nil {
			result.Translate = tr
			if tr.IsInputIsEng() {
				result.Dicts = fetchDictOfWords(tr.Input, true)
			} else if tr.IsTranslatedIsEng() {
				result.Dicts = fetchDictOfWords(tr.Translated, true)
			}

			found = true
		}
		return nil
	})

	return result, found
}

func fetchDictOfWords(engSentense string, onlyFromCache bool) []Dict {

	//TODO
	return nil
}

func fetchDictFromCache(db *bolt.DB, word string) (Dict, bool) {
	var d Dict
	err := db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(BOLT_DICT_BUCKET))
		if bucket == nil {
			return nil
		}
		val := bucket.Get([]byte(word))
		if len(val) == 0 {
			return nil
		}
		err := json.Unmarshal(val, &d)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return d, false
	}
	return d, true
}

func putTranslationToCache(db *bolt.DB, tr Translate) error {
	return db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(BOLT_TRANSLATE_BUCKET))
		if err != nil {
			return err
		}
		d, err := json.Marshal(tr)
		if err != nil {
			return err
		}
		err = bucket.Put([]byte(tr.Input), d)
		return err
	})
}

func putDictToCache(db *bolt.DB, d Dict) error {
	return db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(BOLT_DICT_BUCKET))
		if err != nil {
			return err
		}
		mashalled, err := json.Marshal(d)
		if err != nil {
			return err
		}
		err = bucket.Put([]byte(d.Word), mashalled)
		return err
	})
}

func fetchDictFromAPI(words []string) []Dict {
	var result []Dict
	for _, word := range words {
		defs := readDict(fmt.Sprintf("https://api.datamuse.com/words?sp=%s&md=d", word))
		if len(defs) != 0 {
			syns := readDict(fmt.Sprintf("https://api.datamuse.com/words?rel_syn=%s&md=d", word))
			ants := readDict(fmt.Sprintf("https://api.datamuse.com/words?rel_and=%s&md=d", word))
			result = append(result, Dict{
				Word:       word,
				Definition: defs[0],
				Synonyms:   syns,
				Antonyms:   ants,
			})
		}
	}

	return result
}
func readDict(url string) []Definition {
	r, err := http.Get(url)
	if err != nil {
		return nil
	}
	defer r.Body.Close()

	d, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil
	}

	var defs []Definition
	err = json.Unmarshal(d, &defs)
	if err != nil {
		panic(err)
	}

	return defs
}

type Definition struct {
	Word string   `json:"word"`
	Defs []string `json:"defs"`
}

type Dict struct {
	Word       string       `json:"word"`
	Definition Definition   `json:"definition"`
	Synonyms   []Definition `json:"synonyms"`
	Antonyms   []Definition `json:"antonyms"`
}

func expandFilePath(p string) string {
	trimPath := strings.TrimSpace(p)
	isAbs := filepath.IsAbs(trimPath)
	plainsDirs := strings.Split(trimPath, "/")

	var dirs []string

	for _, plainDir := range plainsDirs {

		if len(plainDir) == 0 {
			continue
		}
		if plainDir == "~" {
			usr, err := user.Current()
			if err != nil {
				panic(err)
			}
			dirs = append(dirs, usr.HomeDir)
		} else if plainDir[0] == '$' {
			dirs = append(dirs, os.Getenv(plainDir[1:]))
		} else {
			dirs = append(dirs, plainDir)
		}
	}

	if isAbs {
		paths := append([]string{"/"}, dirs...)
		absp, err := filepath.Abs(filepath.Join(paths...))
		if err != nil {
			panic(err)
		}
		return absp

	} else {
		absp, err := filepath.Abs(filepath.Join(dirs...))
		if err != nil {
			panic(err)
		}
		return absp
	}

}

type TranslateAndDicts struct {
	Translate Translate `json:"translate"`
	Dicts     []Dict    `json:"dicts"`
}

type Translate struct {
	Input          string `json:"input"`
	InputLang      string `json:"input_lang"`
	Translated     string `json:"translated"`
	TranslatedLang string `json:"translated_lang"`
}

func (tr Translate) IsInputIsEng() bool {
	return tr.InputLang == language.English.String()
}

func (tr Translate) IsTranslatedIsEng() bool {
	return tr.TranslatedLang == language.English.String()
}

func newTranslate(inputLang language.Tag, input string, translatedLang language.Tag, translated string) Translate {
	return Translate{
		Input:          html.UnescapeString(input),
		InputLang:      inputLang.String(),
		Translated:     html.UnescapeString(translated),
		TranslatedLang: translatedLang.String(),
	}
}

func plainPrinter(tr TranslateAndDicts) {
	//TODO
	//fmt.Fprintf(os.Stdout, "%s\n%s\n\n", tr.Input, tr.Translated)
}

func plainSlicePrinter(tr []TranslateAndDicts) {
	for _, each := range tr {
		plainPrinter(each)
	}
}
func jsonPrinter(tr TranslateAndDicts) {
	j, err := json.Marshal(tr)
	if err == nil {
		fmt.Fprintf(os.Stdout, "%s\n", string(j))
	}
}

func jsonSlicePrinter(tr []TranslateAndDicts) {
	j, err := json.Marshal(tr)
	if err == nil {
		fmt.Fprintf(os.Stdout, "%s\n", string(j))
	}
}
