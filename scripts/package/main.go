// Package a Linux skill with executable permissions even when built on Windows.
package main

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func pack(root, output string) error {
	f, err := os.Create(output)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(root), path)
		if err != nil {
			return err
		}
		h, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel)
		h.Mode = 0644
		if info.IsDir() {
			h.Mode = 0755
			h.Name += "/"
		}
		if strings.HasSuffix(h.Name, "/scripts/onekvm") {
			h.Mode = 0755
		}
		if err = tw.WriteHeader(h); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(tw, in)
		return err
	})
	if err != nil {
		return err
	}
	if err = tw.Close(); err != nil {
		return err
	}
	if err = gz.Close(); err != nil {
		return err
	}
	return f.Close()
}
func main() {
	if len(os.Args) != 3 {
		panic("usage: package SKILL_DIRECTORY OUTPUT.tar.gz")
	}
	if err := pack(os.Args[1], os.Args[2]); err != nil {
		panic(err)
	}
}
