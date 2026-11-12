package snapshot

import (
	"bytes"
	"cmp"
	"compress/zlib"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	indexSignature     = "DIRC"
	indexHeaderBytes   = 12
	indexModeAt        = 24
	indexIDAt          = 40
	indexFlagsAt       = indexIDAt + sha1.Size
	indexEntryBytes    = indexFlagsAt + 2
	indexEntryAlign    = 8
	indexTrailerBytes  = sha1.Size
	indexStageBits     = 0x3000
	indexExtendedFlag  = 0x4000
	treeMode           = "40000"
	objectFormatOption = "objectformat"
)

type indexed struct {
	path string
	mode uint32
	id   []byte
	raw  []byte
}

func (f Files) writeTree(ctx context.Context) (string, error) {
	if id, ok := f.writeTreeHere(); ok {
		return id, nil
	}
	tree, err := f.Git(ctx, "", "write-tree")
	return strings.TrimSpace(tree), err
}

func (f Files) Commit(ctx context.Context, message, tree string, parents ...string) (string, error) {
	if !f.sha1Objects() {
		args := []string{"commit-tree", tree}
		for _, parent := range parents {
			args = append(args, "-p", parent)
		}
		commit, err := f.Git(ctx, message, args...)
		return strings.TrimSpace(commit), err
	}
	body := "tree " + tree + "\n"
	for _, parent := range parents {
		body += "parent " + parent + "\n"
	}
	signed := "tofu <tofu@localhost> " + strconv.FormatInt(time.Now().Unix(), 10) + " +0000\n"
	id, err := f.writeObject("commit", []byte(body+"author "+signed+"committer "+signed+"\n"+message))
	return hex.EncodeToString(id), err
}

func (f Files) sha1Objects() bool {
	config, err := os.ReadFile(f.path("config"))
	return err == nil && !bytes.Contains(bytes.ToLower(config), []byte(objectFormatOption))
}

func (f Files) writeTreeHere() (string, bool) {
	if !f.sha1Objects() {
		return "", false
	}
	raw, err := os.ReadFile(f.path(indexName))
	if err != nil {
		return "", false
	}
	entries, ok := readIndex(raw)
	if !ok {
		return "", false
	}
	id, err := f.writeTreeOf(entries, "")
	return hex.EncodeToString(id), err == nil
}

func readIndex(raw []byte) ([]indexed, bool) {
	if len(raw) < indexHeaderBytes+indexTrailerBytes || string(raw[:4]) != indexSignature {
		return nil, false
	}
	if version := binary.BigEndian.Uint32(raw[4:8]); version != 2 && version != 3 {
		return nil, false
	}
	count := binary.BigEndian.Uint32(raw[8:12])
	entries := make([]indexed, 0, count)
	at, end := indexHeaderBytes, len(raw)-indexTrailerBytes
	for range count {
		if at+indexEntryBytes > end {
			return nil, false
		}
		flags := binary.BigEndian.Uint16(raw[at+indexFlagsAt : at+indexEntryBytes])
		named := bytes.IndexByte(raw[at+indexEntryBytes:end], 0)
		if flags&(indexStageBits|indexExtendedFlag) != 0 || named < 0 {
			return nil, false
		}
		next := at + (indexEntryBytes+named+indexEntryAlign)&^(indexEntryAlign-1)
		entries = append(entries, indexed{path: string(raw[at+indexEntryBytes : at+indexEntryBytes+named]),
			mode: binary.BigEndian.Uint32(raw[at+indexModeAt : at+indexModeAt+4]), id: raw[at+indexIDAt : at+indexFlagsAt], raw: raw[at:next]})
		at = next
	}
	for at+8 <= end {
		if signature := string(raw[at : at+4]); signature == "link" || signature == "sdir" {
			return nil, false
		}
		at += 8 + int(binary.BigEndian.Uint32(raw[at+4:at+8]))
	}
	return entries, at == end
}

func joinIndexes(parts [][]byte) ([]byte, bool) {
	var entries []indexed
	for _, raw := range parts {
		if raw == nil {
			continue
		}
		read, ok := readIndex(raw)
		if !ok {
			return nil, false
		}
		entries = append(entries, read...)
	}
	slices.SortFunc(entries, func(a, b indexed) int { return strings.Compare(a.path, b.path) })
	joined := binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32([]byte(indexSignature), 2), uint32(len(entries)))
	for _, one := range entries {
		joined = append(joined, one.raw...)
	}
	sum := sha1.Sum(joined)
	return append(joined, sum[:]...), true
}

func (f Files) writeTreeOf(entries []indexed, prefix string) ([]byte, error) {
	type entry struct {
		mode, name string
		id         []byte
	}
	var tree []entry
	for i := 0; i < len(entries); {
		name, _, nested := strings.Cut(entries[i].path[len(prefix):], "/")
		if !nested {
			tree = append(tree, entry{mode: strconv.FormatUint(uint64(entries[i].mode), 8), name: name, id: entries[i].id})
			i++
			continue
		}
		inside := prefix + name + "/"
		j := i + 1
		for j < len(entries) && strings.HasPrefix(entries[j].path, inside) {
			j++
		}
		id, err := f.writeTreeOf(entries[i:j], inside)
		if err != nil {
			return nil, err
		}
		tree = append(tree, entry{mode: treeMode, name: name + "/", id: id})
		i = j
	}
	slices.SortFunc(tree, func(a, b entry) int { return cmp.Compare(a.name, b.name) })
	var body bytes.Buffer
	for _, one := range tree {
		body.WriteString(one.mode + " " + strings.TrimSuffix(one.name, "/") + "\x00")
		body.Write(one.id)
	}
	return f.writeObject("tree", body.Bytes())
}

func (f Files) writeObject(kind string, body []byte) ([]byte, error) {
	object := append([]byte(kind+" "+strconv.Itoa(len(body))+"\x00"), body...)
	sum := sha1.Sum(object)
	name := hex.EncodeToString(sum[:])
	dir := filepath.Join(f.Dir(), "objects", name[:2])
	if _, err := os.Stat(filepath.Join(dir, name[2:])); err == nil {
		return sum[:], nil
	}
	var packed bytes.Buffer
	deflate := zlib.NewWriter(&packed)
	_, _ = deflate.Write(object)
	if err := deflate.Close(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	temp, err := os.CreateTemp(dir, "tmp_obj_")
	if err != nil {
		return nil, err
	}
	_, err = temp.Write(packed.Bytes())
	if err = errors.Join(err, temp.Close()); err == nil {
		err = os.Rename(temp.Name(), filepath.Join(dir, name[2:]))
	}
	if err != nil {
		_ = os.Remove(temp.Name())
		return nil, err
	}
	return sum[:], nil
}
