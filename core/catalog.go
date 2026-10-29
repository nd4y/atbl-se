package core

import (
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nd4y/atblse/internal/protocol"
)

type catalogRecord struct {
	Version int    `json:"version"`
	UUID    string `json:"uuid"`
	SHA256  string `json:"sha256"`
	Data    []byte `json:"data"`
}

// Captured factory catalog for firmware1.03. No runtime cloud access is needed.
//
//go:embed catalog/factory.json
var factoryCatalog []byte

func factoryReplacement(uuid string) (localReplacement, error) {
	var records []catalogRecord
	if err := json.Unmarshal(factoryCatalog, &records); err != nil {
		return localReplacement{}, err
	}
	for _, record := range records {
		if record.UUID == uuid {
			return validatedCatalogRecord(record, uuid)
		}
	}
	return localReplacement{}, os.ErrNotExist
}

type localReplacement struct {
	token  string
	id     uint32
	binary []byte
	peak   int
}

func nativeCatalogData(data []byte) (localReplacement, error) {
	if len(data) < 14 || (len(data)-8)%6 != 0 || (len(data)-8)/6 > 99999 || binary.LittleEndian.Uint32(data[4:8]) != 1 {
		return localReplacement{}, errors.New("invalid catalog binary layout")
	}
	id := binary.LittleEndian.Uint32(data[:4])
	if id == 0 {
		return localReplacement{}, errors.New("catalog ID must be nonzero")
	}
	peak := 0
	var previous uint32
	for offset := 8; offset < len(data); offset += 6 {
		at := binary.LittleEndian.Uint32(data[offset : offset+4])
		op, value := data[offset+4], data[offset+5]
		if at > protocol.MaxScriptTime || at < previous || op > 2 || op == 0 && value > 100 {
			return localReplacement{}, errors.New("unsupported catalog event or timeline")
		}
		previous = at
		if op == 0 && int(value) > peak {
			peak = int(value)
		}
	}
	return localReplacement{token: strconv.FormatUint(uint64(id), 10), id: id, binary: append([]byte(nil), data...), peak: peak}, nil
}

// ImportLocalScript stores a caller-supplied native catalog file for offline use.
// It preserves header ID and valve events. No network or device action occurs.
func ImportLocalScript(dataDir, uuid string, data []byte) error {
	if dataDir == "" || !protocol.ValidScriptToken(uuid) {
		return errors.New("data directory and canonical UUID required")
	}
	if _, err := nativeCatalogData(data); err != nil {
		return err
	}
	dir := filepath.Join(dataDir, "local-catalog")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	// Across CLI processes as well as goroutines: reserve quota and replace as
	// one transaction. A failed import leaves the previous file intact.
	lock := filepath.Join(dir, ".import-lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		return errors.New("catalog import locked; ensure no other import is running before removing local-catalog/.import-lock")
	}
	defer os.Remove(lock)
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	count := 0
	existing := false
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".json") || strings.HasPrefix(f.Name(), ".pending-") {
			count++
		}
		if f.Name() == uuid+".json" {
			existing = true
		}
	}
	if !existing && count >= maxStoredScripts {
		return errors.New("local catalog is full")
	}
	sum := sha256.Sum256(data)
	raw, err := json.Marshal(catalogRecord{1, uuid, hex.EncodeToString(sum[:]), data})
	if err != nil {
		return err
	}
	return writeStateFile(filepath.Join(dir, uuid+".json"), raw, true)
}

func readLocalCatalog(dataDir, uuid string) (localReplacement, error) {
	if !protocol.ValidScriptToken(uuid) {
		return localReplacement{}, errors.New("invalid catalog UUID")
	}
	path := filepath.Join(dataDir, "local-catalog", uuid+".json")
	info, err := os.Lstat(path)
	if err != nil {
		return localReplacement{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxScriptBytes {
		return localReplacement{}, errors.New("invalid catalog record")
	}
	f, err := os.Open(path)
	if err != nil {
		return localReplacement{}, err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, maxScriptBytes+1))
	d.DisallowUnknownFields()
	var record catalogRecord
	if err = d.Decode(&record); err != nil {
		return localReplacement{}, err
	}
	if d.Decode(new(any)) != io.EOF {
		return localReplacement{}, errors.New("invalid catalog metadata")
	}
	return validatedCatalogRecord(record, uuid)
}

func validatedCatalogRecord(record catalogRecord, uuid string) (localReplacement, error) {
	if record.Version != 1 || record.UUID != uuid {
		return localReplacement{}, errors.New("invalid catalog metadata")
	}
	sum := sha256.Sum256(record.Data)
	if hex.EncodeToString(sum[:]) != record.SHA256 {
		return localReplacement{}, errors.New("catalog checksum mismatch")
	}
	return nativeCatalogData(record.Data)
}

func (s *Server) localReplacement(uuid string) (localReplacement, error) {
	data, err := readLocalCatalog(s.cfg.DataDir, uuid)
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		return data, err
	}
	data, err = factoryReplacement(uuid)
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		return data, err
	}
	syncData, err := s.scripts.get(uuid)
	if err != nil {
		return localReplacement{}, err
	}
	id := localScriptID(uuid)
	binary.LittleEndian.PutUint32(syncData.binary[:4], id)
	return localReplacement{token: uuid, id: id, binary: syncData.binary, peak: syncData.peak}, nil
}
