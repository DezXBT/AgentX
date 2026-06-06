package transaction

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var digitsRegex = regexp.MustCompile(`\d+`)

// getKey extracts the twitter-site-verification key from the home page.
func getKey(doc *goquery.Document) (string, error) {
	v, ok := doc.Find("meta[name='twitter-site-verification']").First().Attr("content")
	if !ok || v == "" {
		return "", fmt.Errorf("transaction: couldn't get [twitter-site-verification] key from page source")
	}
	return v, nil
}

// getFrames returns the loading-x-anim animation frame elements.
func getFrames(doc *goquery.Document) *goquery.Selection {
	return doc.Find("[id^='loading-x-anim']")
}

// get2DArray ports ClientTransaction.get_2d_array. It selects the frame at
// keyBytes[5]%4, reads the second path's "d" attribute, drops the first 9
// characters, splits on "C", and parses each segment's digit runs.
func get2DArray(doc *goquery.Document, keyBytes []int) ([][]int, error) {
	frames := getFrames(doc)
	if frames.Length() == 0 {
		return nil, fmt.Errorf("transaction: no loading-x-anim frames found")
	}
	frame := frames.Eq(keyBytes[5] % 4)
	d, ok := frame.Children().Eq(0).Children().Eq(1).Attr("d")
	if !ok || len(d) < 9 {
		return nil, fmt.Errorf("transaction: animation frame path data missing or too short")
	}
	segments := strings.Split(d[9:], "C")
	out := make([][]int, 0, len(segments))
	for _, seg := range segments {
		nums := digitsRegex.FindAllString(seg, -1)
		row := make([]int, 0, len(nums))
		for _, n := range nums {
			i, _ := strconv.Atoi(n)
			row = append(row, i)
		}
		out = append(out, row)
	}
	return out, nil
}

// getIndices ports ClientTransaction.get_indices over the ondemand.s text.
func getIndices(ondemandText string) (rowIndex int, keyBytesIndices []int, err error) {
	matches := indicesRegex.FindAllStringSubmatch(ondemandText, -1)
	if len(matches) == 0 {
		return 0, nil, fmt.Errorf("transaction: couldn't get KEY_BYTE indices")
	}
	all := make([]int, 0, len(matches))
	for _, m := range matches {
		i, convErr := strconv.Atoi(m[1])
		if convErr != nil {
			return 0, nil, fmt.Errorf("transaction: bad index %q: %w", m[1], convErr)
		}
		all = append(all, i)
	}
	return all[0], all[1:], nil
}

// getOndemandFileURL ports utils.get_ondemand_file_url over the raw home HTML.
func getOndemandFileURL(homeHTML string) (string, error) {
	idxMatch := onDemandFileRegex.FindStringSubmatch(homeHTML)
	if idxMatch == nil {
		return "", fmt.Errorf("transaction: couldn't locate ondemand.s file index")
	}
	hashMatch := onDemandHashPattern(idxMatch[1]).FindStringSubmatch(homeHTML)
	if hashMatch == nil {
		return "", fmt.Errorf("transaction: couldn't locate ondemand.s file hash")
	}
	return fmt.Sprintf(onDemandFileURLTemplate, hashMatch[1]), nil
}
