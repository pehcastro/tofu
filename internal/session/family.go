package session

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	FamilyMark       = "#"
	GenerationMark   = "."
	familyTagLength  = 5
	familyTagBase    = 36
	shortestIDEnding = 6
)

type Identity struct {
	Session    string    `json:"session"`
	Family     string    `json:"family"`
	Tag        string    `json:"tag"`
	Name       string    `json:"name,omitempty"`
	Generation int       `json:"generation"`
	Started    time.Time `json:"started"`
}

func (i Identity) Handle() string {
	return cmp.Or(i.Name, strings.TrimPrefix(i.Family, IDPrefix)) + FamilyMark + i.Tag + GenerationMark + strconv.Itoa(i.Generation)
}

func FamilyTag(family string) string {
	sum := sha256.Sum256([]byte(family))
	tag := strconv.FormatUint(binary.BigEndian.Uint64(sum[:])|1<<63, familyTagBase)
	return tag[len(tag)-familyTagLength:]
}

func identityOf(header, first Header, generation int) Identity {
	return Identity{Session: header.ID, Family: first.ID, Tag: FamilyTag(first.ID), Name: first.Named(), Generation: generation, Started: first.At}
}

func (s *Store) Identity(id string) (Identity, error) {
	header, err := s.read(id)
	if err != nil {
		return Identity{}, err
	}
	if header.Generation > 0 {
		if first, err := s.read(header.Root); err == nil {
			return identityOf(header, first, header.Generation), nil
		}
	}
	chain, err := s.Ancestors(id)
	if err != nil {
		return Identity{}, err
	}
	first := header
	if len(chain) > 0 {
		first = chain[len(chain)-1]
	}
	return identityOf(header, first, len(chain)+1), nil
}

type Family struct {
	Identity
	Generations  []Header
	Branches     []string
	BranchedFrom *Carried
}

func (f Family) Head() Header { return f.Generations[len(f.Generations)-1] }

func (l Listing) Families() []Family {
	byID := map[string]Header{}
	for _, header := range l.Sessions {
		byID[header.ID] = header
	}
	members := map[string][]Header{}
	for _, header := range l.Sessions {
		chain := []Header{header}
		for parent, found := byID[header.Parent]; found && !slices.ContainsFunc(chain, func(seen Header) bool { return seen.ID == parent.ID }); parent, found = byID[parent.Parent] {
			chain = append(chain, parent)
		}
		first := chain[len(chain)-1].ID
		members[first] = append(members[first], header)
	}
	families := make([]Family, 0, len(members))
	familyOf := map[string]int{}
	for first, generations := range members {
		slices.SortFunc(generations, func(a, b Header) int { return a.At.Compare(b.At) })
		head := generations[len(generations)-1]
		for _, header := range generations {
			familyOf[header.ID] = len(families)
		}
		families = append(families, Family{Identity: identityOf(head, byID[first], len(generations)), Generations: generations, BranchedFrom: byID[first].BranchedFrom})
	}
	for _, family := range families {
		if family.BranchedFrom == nil {
			continue
		}
		if at, found := familyOf[family.BranchedFrom.Session]; found {
			families[at].Branches = append(families[at].Branches, family.Family)
		}
	}
	slices.SortFunc(families, func(a, b Family) int { return b.Head().At.Compare(a.Head().At) })
	return families
}

func resolveIn(families []Family, handle string) []Header {
	var found []Header
	for _, family := range families {
		for _, header := range slices.Backward(family.Generations) {
			if header.Named() == handle {
				found = append(found, header)
				break
			}
		}
	}
	if len(found) > 0 {
		return found
	}
	_, ref, marked := strings.Cut(handle, FamilyMark)
	if !marked {
		ref = handle
	}
	tag, number, numbered := strings.Cut(ref, GenerationMark)
	for _, family := range families {
		if family.Tag != tag {
			continue
		}
		if !numbered {
			return []Header{family.Head()}
		}
		if n, err := strconv.Atoi(number); err == nil && n >= 1 && n <= len(family.Generations) {
			return []Header{family.Generations[n-1]}
		}
		return nil
	}
	if len(ref) < shortestIDEnding {
		return nil
	}
	for _, family := range families {
		found = append(found, slices.DeleteFunc(slices.Clone(family.Generations), func(header Header) bool { return !DrawnAs(header.ID, ref) })...)
	}
	return found
}

func (l Listing) FamilyOf(id string) (Family, []Family, error) {
	families := l.Families()
	for _, family := range families {
		if slices.ContainsFunc(family.Generations, func(header Header) bool { return header.ID == id }) {
			return family, families, nil
		}
	}
	return Family{}, families, fmt.Errorf("session: %s is in no family here: %w", id, fs.ErrNotExist)
}
