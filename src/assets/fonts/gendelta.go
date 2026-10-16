//go:build ignore

// gendelta copies each family's regular face from a Nerd Fonts release
// directory and writes its bold/italic faces as gzipped deltas against it
// (the format applyDelta in fonts.go reads).
//
//	go run gendelta.go -src <dir of release .ttf files> JetBrainsMonoNerdFont ...
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"flag"
	"hash/crc32"
	"hash/maphash"
	"log"
	"os"
	"path/filepath"
)

const block = 24

func delta(base, tgt []byte) []byte {
	seed := maphash.MakeSeed()
	idx := map[uint64]int{}
	for i := 0; i+block <= len(base); i += 4 {
		h := maphash.Bytes(seed, base[i:i+block])
		if _, ok := idx[h]; !ok {
			idx[h] = i
		}
	}
	out := binary.AppendUvarint(nil, uint64(len(tgt)))
	out = binary.LittleEndian.AppendUint32(out, crc32.ChecksumIEEE(tgt))
	var lit []byte
	last := 0
	for i := 0; i < len(tgt); {
		if i+block <= len(tgt) {
			j, ok := idx[maphash.Bytes(seed, tgt[i:i+block])]
			if ok && bytes.Equal(base[j:j+block], tgt[i:i+block]) {
				n := block
				for i+n < len(tgt) && j+n < len(base) && tgt[i+n] == base[j+n] {
					n++
				}
				k := 0
				for k < len(lit) && j-k > 0 && base[j-k-1] == lit[len(lit)-1-k] {
					k++
				}
				lit = lit[:len(lit)-k]
				i, j, n = i-k, j-k, n+k
				out = binary.AppendUvarint(out, uint64(len(lit)))
				out = append(out, lit...)
				lit = lit[:0]
				out = binary.AppendVarint(out, int64(j-last))
				out = binary.AppendUvarint(out, uint64(n))
				last = j + n
				i += n
				continue
			}
		}
		lit = append(lit, tgt[i])
		i++
	}
	out = binary.AppendUvarint(out, uint64(len(lit)))
	return append(out, lit...)
}

func main() {
	src := flag.String("src", ".", "directory holding the release .ttf files")
	flag.Parse()
	for _, family := range flag.Args() {
		base, err := os.ReadFile(filepath.Join(*src, family+"-Regular.ttf"))
		if err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(family+"-Regular.ttf", base, 0o644); err != nil {
			log.Fatal(err)
		}
		for _, style := range []string{"Bold", "Italic", "BoldItalic"} {
			tgt, err := os.ReadFile(filepath.Join(*src, family+"-"+style+".ttf"))
			if os.IsNotExist(err) {
				continue
			} else if err != nil {
				log.Fatal(err)
			}
			var buf bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			zw.Write(delta(base, tgt))
			zw.Close()
			if err := os.WriteFile(family+"-"+style+".delta.gz", buf.Bytes(), 0o644); err != nil {
				log.Fatal(err)
			}
		}
	}
}
