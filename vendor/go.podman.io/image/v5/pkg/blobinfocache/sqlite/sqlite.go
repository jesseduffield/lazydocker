// Package boltdb implements a BlobInfoCache backed by SQLite.
package sqlite

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3" // Registers the "sqlite3" backend backend for database/sql
	"github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/internal/blobinfocache"
	"go.podman.io/image/v5/pkg/blobinfocache/internal/prioritize"
	"go.podman.io/image/v5/types"
)

const (
	// NOTE: There is no versioning data inside the file; this is a “cache”, so on an incompatible format upgrade
	// we can simply start over with a different filename; update blobInfoCacheFilename.
	// That also means we don’t have to worry about co-existing readers/writers which know different versions of the schema
	// (which would require compatibility in both directions).

	// Assembled sqlite options used when opening the database.
	sqliteOptions = "?" +
		// Deal with timezone automatically.
		// go-sqlite3 always _records_ timestamps as a text: time in local time + a time zone offset.
		// _loc affects how the values are _parsed_: (which timezone is assumed for numeric timestamps or for text which does not specify an offset, or)
		// if the time zone offset matches the specified time zone, the timestamp is assumed to be in that time zone / location;
		// (otherwise an unnamed time zone carrying just a hard-coded offset, but no location / DST rules is used).
		"_loc=auto" +
		// Force an fsync after each transaction (https://www.sqlite.org/pragma.html#pragma_synchronous).
		"&_sync=FULL" +
		// Allow foreign keys (https://www.sqlite.org/pragma.html#pragma_foreign_keys).
		// We don’t currently use any foreign keys, but this is a good choice long-term (not default in SQLite only for historical reasons).
		"&_foreign_keys=1" +
		// Use BEGIN EXCLUSIVE (https://www.sqlite.org/lang_transaction.html);
		// i.e. obtain a write lock for _all_ transactions at the transaction start (never use a read lock,
		// never upgrade from a read to a write lock - that can fail if multiple read lock owners try to do that simultaneously).
		//
		// This, together with go-sqlite3’s default for _busy_timeout=5000, means that we should never see a “database is locked” error,
		// the database should block on the exclusive lock when starting a transaction, and the problematic case of two simultaneous
		// holders of a read lock trying to upgrade to a write lock (and one necessarily failing) is prevented.
		// Compare https://github.com/mattn/go-sqlite3/issues/274 .
		//
		// Ideally the BEGIN / BEGIN EXCLUSIVE decision could be made per-transaction, compare https://github.com/mattn/go-sqlite3/pull/1167
		// or https://github.com/mattn/go-sqlite3/issues/400 .
		// The currently-proposed  workaround is to create two different SQL “databases” (= connection pools) with different _txlock settings,
		// which seems rather wasteful.
		"&_txlock=exclusive"
)

// cache is a BlobInfoCache implementation which uses a SQLite file at the specified path.
type cache struct {
	path string

	// The database/sql package says “It is rarely necessary to close a DB.”, and steers towards a long-term *sql.DB connection pool.
	// That’s probably very applicable for database-backed services, where the database is the primary data store. That’s not necessarily
	// the case for callers of c/image, where image operations might be a small proportion of the total runtime, and the cache is fairly
	// incidental even to the image operations. It’s also hard for us to use that model, because the public BlobInfoCache object doesn’t have
	// a Close method, so creating a lot of single-use caches could leak data.
	//
	// Instead, the private BlobInfoCache2 interface provides Open/Close methods, and they are called by c/image/copy.Image.
	// This amortizes the cost of opening/closing the SQLite state over a single image copy, while keeping no long-term resources open.
	// Some rough benchmarks in https://github.com/containers/image/pull/2092 suggest relative costs on the order of "25" for a single
	// *sql.DB left open long-term, "27" for a *sql.DB open for a single image copy, and "40" for opening/closing a *sql.DB for every
	// single transaction; so the Open/Close per image copy seems a reasonable compromise (especially compared to the previous implementation,
	// somewhere around "700").

	lock sync.Mutex
	// The following fields can only be accessed with lock held.
	refCount int     // number of outstanding Open() calls
	db       *sql.DB // nil if not set (may happen even if refCount > 0 on errors)
}

// New returns BlobInfoCache implementation which uses a SQLite file at path.
//
// Most users should call blobinfocache.DefaultCache instead.
func New(path string) (types.BlobInfoCache, error) {
	return new2(path)
}

func new2(path string) (*cache, error) {
	db, err := rawOpen(path)
	if err != nil {
		return nil, fmt.Errorf("initializing blob info cache at %q: %w", path, err)
	}
	err = func() (retErr error) { // A scope for defer
		defer func() {
			closeErr := db.Close()
			if retErr == nil {
				retErr = closeErr
			}
		}()
		// We don’t check the schema before every operation, because that would be costly
		// and because we assume schema changes will be handled by using a different path.
		return ensureDBHasCurrentSchema(db)
	}()
	if err != nil {
		return nil, err
	}
	return &cache{
		path:     path,
		refCount: 0,
		db:       nil,
	}, nil
}

// rawOpen returns a new *sql.DB for path.
// The caller should arrange for it to be .Close()d.
func rawOpen(path string) (*sql.DB, error) {
	// This exists to centralize the use of sqliteOptions.
	return sql.Open("sqlite3", path+sqliteOptions)
}

// Open() sets up the cache for future accesses, potentially acquiring costly state. Each Open() must be paired with a Close().
// Note that public callers may call the types.BlobInfoCache operations without Open()/Close().
func (sqc *cache) Open() {
	sqc.lock.Lock()
	defer sqc.lock.Unlock()

	if sqc.refCount == 0 {
		db, err := rawOpen(sqc.path)
		if err != nil {
			logrus.Warnf("Error opening (previously-successfully-opened) blob info cache at %q: %v", sqc.path, err)
			db = nil // But still increase sqc.refCount, because a .Close() will happen
		}
		sqc.db = db
	}
	sqc.refCount++
}

// Close destroys state created by Open().
func (sqc *cache) Close() {
	sqc.lock.Lock()
	defer sqc.lock.Unlock()

	switch sqc.refCount {
	case 0:
		logrus.Errorf("internal error using pkg/blobinfocache/sqlite.cache: Close() without a matching Open()")
		return
	case 1:
		if sqc.db != nil {
			sqc.db.Close()
			sqc.db = nil
		}
	}
	sqc.refCount--
}

type void struct{} // So that we don’t have to write struct{}{} all over the place

// transaction calls fn within a read-write transaction in sqc.
func transaction[T any](sqc *cache, fn func(tx *sql.Tx) (T, error)) (_ T, retErr error) {
	db, closeDB, err := func() (*sql.DB, func() error, error) { // A scope for defer
		sqc.lock.Lock()
		defer sqc.lock.Unlock()

		if sqc.db != nil {
			return sqc.db, func() error { return nil }, nil
		}
		db, err := rawOpen(sqc.path)
		if err != nil {
			return nil, nil, fmt.Errorf("opening blob info cache at %q: %w", sqc.path, err)
		}
		return db, db.Close, nil
	}()
	if err != nil {
		var zeroRes T // A zero value of T
		return zeroRes, err
	}
	defer func() {
		closeErr := closeDB()
		if retErr == nil {
			retErr = closeErr
		}
	}()

	return dbTransaction(db, fn)
}

// dbTransaction calls fn within a read-write transaction in db.
func dbTransaction[T any](db *sql.DB, fn func(tx *sql.Tx) (T, error)) (T, error) {
	// Ideally we should be able to distinguish between read-only and read-write transactions, see the _txlock=exclusive discussion.

	var zeroRes T // A zero value of T

	tx, err := db.Begin()
	if err != nil {
		return zeroRes, fmt.Errorf("beginning transaction: %w", err)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction: %v", err)
			}
		}
	}()

	res, err := fn(tx)
	if err != nil {
		return zeroRes, err
	}
	if err := tx.Commit(); err != nil {
		return zeroRes, fmt.Errorf("committing transaction: %w", err)
	}

	succeeded = true
	return res, nil
}

// querySingleValue executes a SELECT which is expected to return at most one row with a single column.
// It returns (value, true, nil) on success, or (value, false, nil) if no row was returned.
func querySingleValue[T any](tx *sql.Tx, query string, params ...any) (T, bool, error) {
	var value T
	if err := tx.QueryRow(query, params...).Scan(&value); err != nil {
		var zeroValue T // A zero value of T
		if errors.Is(err, sql.ErrNoRows) {
			return zeroValue, false, nil
		}
		return zeroValue, false, err
	}
	return value, true, nil
}

// ensureDBHasCurrentSchema adds the necessary tables and indices to a database.
// This is typically used when creating a previously-nonexistent database.
// We don’t really anticipate schema migrations; with c/image usually vendored, not using
// shared libraries, migrating a schema on an existing database would affect old-version users.
// Instead, schema changes are likely to be implemented by using a different cache file name,
// and leaving existing caches around for old users.
func ensureDBHasCurrentSchema(db *sql.DB) error {
	// Considered schema design alternatives:
	//
	// (Overall, considering the overall network latency and disk I/O costs of many-megabyte layer pulls which are happening while referring
	// to the blob info cache, it seems reasonable to prioritize readability over microoptimization of this database.)
	//
	// * This schema uses the text representation of digests.
	//
	//   We use the fairly wasteful text with hexadecimal digits because digest.Digest does not define a binary representation;
	//   and the way digest.Digest.Hex() is deprecated in favor of digest.Digest.Encoded(), and the way digest.Algorithm
	//   is documented to “define the string encoding” suggests that assuming a hexadecimal representation and turning that
	//   into binary ourselves is not a good idea in general; we would have to special-case the currently-known algorithm
	//   — and that would require us to implement two code paths, one of them basically never exercised / never tested.
	//
	// * There are two separate items for recording the uncompressed digest and digest compressors.
	//   Alternatively, we could have a single "digest facts" table with NULLable columns.
	//
	//   The way the BlobInfoCache API works, we are only going to write one value at a time, so
	//   sharing a table would not be any more efficient for writes (same number of lookups, larger row tuples).
	//   Reads in candidateLocations would not be more efficient either, the searches in DigestCompressors and DigestUncompressedPairs
	//   do not coincide (we want a compressor for every candidate, but the uncompressed digest only for the primary digest; and then
	//   we search in DigestUncompressedPairs by uncompressed digest, not by the primary key).
	//
	//   Also, using separate items allows the single-item writes to be done using a simple INSERT OR REPLACE, instead of having to
	//   do a more verbose ON CONFLICT(…) DO UPDATE SET … = ….
	//
	// * Joins (the two that exist in appendReplacementCandidates) are based on the text representation of digests.
	//
	//   Using integer primary keys might make the joins themselves a bit more efficient, but then we would need to involve an extra
	//   join to translate from/to the user-provided digests anyway. If anything, that extra join (potentially more btree lookups)
	//   is probably costlier than comparing a few more bytes of data.
	//
	//   Perhaps more importantly, storing digest texts directly makes the database dumps much easier to read for humans without
	//   having to do extra steps to decode the integers into digest values (either by running sqlite commands with joins, or mentally).
	//
	items := []struct{ itemName, command string }{
		{
			"DigestUncompressedPairs",
			`CREATE TABLE IF NOT EXISTS DigestUncompressedPairs(` +
				// index implied by PRIMARY KEY
				`anyDigest			TEXT PRIMARY KEY NOT NULL,` +
				// DigestUncompressedPairs_index_uncompressedDigest
				`uncompressedDigest	TEXT NOT NULL
			)`,
		},
		{
			"DigestUncompressedPairs_index_uncompressedDigest",
			`CREATE INDEX IF NOT EXISTS DigestUncompressedPairs_index_uncompressedDigest ON DigestUncompressedPairs(uncompressedDigest)`,
		},
		{
			"DigestCompressors",
			`CREATE TABLE IF NOT EXISTS DigestCompressors(` +
				// index implied by PRIMARY KEY
				`digest		TEXT PRIMARY KEY NOT NULL,` +
				// May include blobinfocache.Uncompressed (not blobinfocache.UnknownCompression).
				`compressor	TEXT NOT NULL
			)`,
		},
		{
			"KnownLocations",
			`CREATE TABLE IF NOT EXISTS KnownLocations(
				transport	TEXT NOT NULL,
				scope 		TEXT NOT NULL,
				digest		TEXT NOT NULL,
				location	TEXT NOT NULL,` +
				// TIMESTAMP is parsed by SQLITE as a NUMERIC affinity, but go-sqlite3 stores text in the (Go formatting semantics)
				// format "2006-01-02 15:04:05.999999999-07:00".
				// See also the _loc option in the sql.Open data source name.
				`time		TIMESTAMP NOT NULL,` +
				// Implies an index.
				// We also search by (transport, scope, digest), that doesn’t need an extra index
				// because it is a prefix of the implied primary-key index.
				`PRIMARY KEY (transport, scope, digest, location)
			)`,
		},
		{
			"DigestTOCUncompressedPairs",
			`CREATE TABLE IF NOT EXISTS DigestTOCUncompressedPairs(` +
				// index implied by PRIMARY KEY
				`tocDigest			TEXT PRIMARY KEY NOT NULL,` +
				`uncompressedDigest	TEXT NOT NULL
			)`,
		},
		{
			"DigestSpecificVariantCompressors", // If changing the schema incompatibly, merge this with DigestCompressors.
			`CREATE TABLE IF NOT EXISTS DigestSpecificVariantCompressors(` +
				// index implied by PRIMARY KEY
				`digest		TEXT PRIMARY KEY NOT NULL,` +
				// The compressor is not `UnknownCompression`.
				`specificVariantCompressor	TEXT NOT NULL,
				specificVariantAnnotations	BLOB NOT NULL
			)`,
		},
	}

	_, err := dbTransaction(db, func(tx *sql.Tx) (void, error) {
		// If the the last-created item exists, assume nothing needs to be done.
		lastItemName := items[len(items)-1].itemName
		_, found, err := querySingleValue[int](tx, "SELECT 1 FROM sqlite_schema WHERE name=?", lastItemName)
		if err != nil {
			return void{}, fmt.Errorf("checking if SQLite schema item %q exists: %w", lastItemName, err)
		}
		if !found {
			// Item does not exist, assuming a fresh database.
			for _, i := range items {
				if _, err := tx.Exec(i.command); err != nil {
					return void{}, fmt.Errorf("creating item %s: %w", i.itemName, err)
				}
			}
		}
		return void{}, nil
	})
	return err
}

// uncompressedDigest implements types.BlobInfoCache.UncompressedDigest within a transaction.
func (sqc *cache) uncompressedDigest(tx *sql.Tx, anyDigest digest.Digest) (digest.Digest, error) {
	uncompressedString, found, err := querySingleValue[string](tx, "SELECT uncompressedDigest FROM DigestUncompressedPairs WHERE anyDigest = ?", anyDigest.String())
	if err != nil {
		return "", err
	}
	if found {
		d, err := digest.Parse(uncompressedString)
		if err != nil {
			return "", err
		}
		return d, nil

	}
	// A record as uncompressedDigest implies that anyDigest must already refer to an uncompressed digest.
	// This way we don't have to waste storage space with trivial (uncompressed, uncompressed) mappings
	// when we already record a (compressed, uncompressed) pair.
	_, found, err = querySingleValue[int](tx, "SELECT 1 FROM DigestUncompressedPairs WHERE uncompressedDigest = ?", anyDigest.String())
	if err != nil {
		return "", err
	}
	if found {
		return anyDigest, nil
	}
	return "", nil
}

// UncompressedDigest returns an uncompressed digest corresponding to anyDigest.
// May return anyDigest if it is known to be uncompressed.
// Returns "" if nothing is known about the digest (it may be compressed or uncompressed).
func (sqc *cache) UncompressedDigest(anyDigest digest.Digest) digest.Digest {
	res, err := transaction(sqc, func(tx *sql.Tx) (digest.Digest, error) {
		return sqc.uncompressedDigest(tx, anyDigest)
	})
	if err != nil {
		return "" // FIXME? Log err (but throttle the log volume on repeated accesses)?
	}
	return res
}

// RecordDigestUncompressedPair records that the uncompressed version of anyDigest is uncompressed.
// It’s allowed for anyDigest == uncompressed.
// WARNING: Only call this for LOCALLY VERIFIED data; don’t record a digest pair just because some remote author claims so (e.g.
// because a manifest/config pair exists); otherwise the cache could be poisoned and allow substituting unexpected blobs.
// (Eventually, the DiffIDs in image config could detect the substitution, but that may be too late, and not all image formats contain that data.)
func (sqc *cache) RecordDigestUncompressedPair(anyDigest digest.Digest, uncompressed digest.Digest) {
	_, _ = transaction(sqc, func(tx *sql.Tx) (void, error) {
		previousString, gotPrevious, err := querySingleValue[string](tx, "SELECT uncompressedDigest FROM DigestUncompressedPairs WHERE anyDigest = ?", anyDigest.String())
		if err != nil {
			return void{}, fmt.Errorf("looking for uncompressed digest for %q", anyDigest)
		}
		if gotPrevious {
			previous, err := digest.Parse(previousString)
			if err != nil {
				return void{}, err
			}
			if previous != uncompressed {
				logrus.Warnf("Uncompressed digest for blob %s previously recorded as %s, now %s", anyDigest, previous, uncompressed)
			}
		}
		if _, err := tx.Exec("INSERT OR REPLACE INTO DigestUncompressedPairs(anyDigest, uncompressedDigest) VALUES (?, ?)",
			anyDigest.String(), uncompressed.String()); err != nil {
			return void{}, fmt.Errorf("recording uncompressed digest %q for %q: %w", uncompressed, anyDigest, err)
		}
		return void{}, nil
	}) // FIXME? Log error (but throttle the log volume on repeated accesses)?
}

// UncompressedDigestForTOC returns an uncompressed digest corresponding to anyDigest.
// Returns "" if the uncompressed digest is unknown.
func (sqc *cache) UncompressedDigestForTOC(tocDigest digest.Digest) digest.Digest {
	res, err := transaction(sqc, func(tx *sql.Tx) (digest.Digest, error) {
		uncompressedString, found, err := querySingleValue[string](tx, "SELECT uncompressedDigest FROM DigestTOCUncompressedPairs WHERE tocDigest = ?", tocDigest.String())
		if err != nil {
			return "", err
		}
		if found {
			d, err := digest.Parse(uncompressedString)
			if err != nil {
				return "", err
			}
			return d, nil

		}
		return "", nil
	})
	if err != nil {
		return "" // FIXME? Log err (but throttle the log volume on repeated accesses)?
	}
	return res
}

// RecordTOCUncompressedPair records that the tocDigest corresponds to uncompressed.
// WARNING: Only call this for LOCALLY VERIFIED data; don’t record a digest pair just because some remote author claims so (e.g.
// because a manifest/config pair exists); otherwise the cache could be poisoned and allow substituting unexpected blobs.
// (Eventually, the DiffIDs in image config could detect the substitution, but that may be too late, and not all image formats contain that data.)
func (sqc *cache) RecordTOCUncompressedPair(tocDigest digest.Digest, uncompressed digest.Digest) {
	_, _ = transaction(sqc, func(tx *sql.Tx) (void, error) {
		previousString, gotPrevious, err := querySingleValue[string](tx, "SELECT uncompressedDigest FROM DigestTOCUncompressedPairs WHERE tocDigest = ?", tocDigest.String())
		if err != nil {
			return void{}, fmt.Errorf("looking for uncompressed digest for blob with TOC %q", tocDigest)
		}
		if gotPrevious {
			previous, err := digest.Parse(previousString)
			if err != nil {
				return void{}, err
			}
			if previous != uncompressed {
				logrus.Warnf("Uncompressed digest for blob with TOC %q previously recorded as %q, now %q", tocDigest, previous, uncompressed)
			}
		}
		if _, err := tx.Exec("INSERT OR REPLACE INTO DigestTOCUncompressedPairs(tocDigest, uncompressedDigest) VALUES (?, ?)",
			tocDigest.String(), uncompressed.String()); err != nil {
			return void{}, fmt.Errorf("recording uncompressed digest %q for blob with TOC %q: %w", uncompressed, tocDigest, err)
		}
		return void{}, nil
	}) // FIXME? Log error (but throttle the log volume on repeated accesses)?
}

// RecordKnownLocation records that a blob with the specified digest exists within the specified (transport, scope) scope,
// and can be reused given the opaque location data.
func (sqc *cache) RecordKnownLocation(transport types.ImageTransport, scope types.BICTransportScope, digest digest.Digest, location types.BICLocationReference) {
	_, _ = transaction(sqc, func(tx *sql.Tx) (void, error) {
		if _, err := tx.Exec("INSERT OR REPLACE INTO KnownLocations(transport, scope, digest, location, time) VALUES (?, ?, ?, ?, ?)",
			transport.Name(), scope.Opaque, digest.String(), location.Opaque, time.Now()); err != nil { // Possibly overwriting an older entry.
			return void{}, fmt.Errorf("recording known location %q for (%q, %q, %q): %w",
				location.Opaque, transport.Name(), scope.Opaque, digest.String(), err)
		}
		return void{}, nil
	}) // FIXME? Log error (but throttle the log volume on repeated accesses)?
}

// RecordDigestCompressorData records data for the blob with the specified digest.
// WARNING: Only call this with LOCALLY VERIFIED data:
//   - don’t record a compressor for a digest just because some remote author claims so
//     (e.g. because a manifest says so);
//   - don’t record the non-base variant or annotations if we are not _sure_ that the base variant
//     and the blob’s digest match the non-base variant’s annotations (e.g. because we saw them
//     in a manifest)
//
// otherwise the cache could be poisoned and cause us to make incorrect edits to type
// information in a manifest.
func (sqc *cache) RecordDigestCompressorData(anyDigest digest.Digest, data blobinfocache.DigestCompressorData) {
	_, _ = transaction(sqc, func(tx *sql.Tx) (void, error) {
		previous, gotPrevious, err := querySingleValue[string](tx, "SELECT compressor FROM DigestCompressors WHERE digest = ?", anyDigest.String())
		if err != nil {
			return void{}, fmt.Errorf("looking for compressor of %q", anyDigest)
		}
		warned := false
		if gotPrevious && previous != data.BaseVariantCompressor {
			logrus.Warnf("Compressor for blob with digest %s previously recorded as %s, now %s", anyDigest, previous, data.BaseVariantCompressor)
			warned = true
		}
		if data.BaseVariantCompressor == blobinfocache.UnknownCompression {
			if _, err := tx.Exec("DELETE FROM DigestCompressors WHERE digest = ?", anyDigest.String()); err != nil {
				return void{}, fmt.Errorf("deleting compressor for digest %q: %w", anyDigest, err)
			}
			if _, err := tx.Exec("DELETE FROM DigestSpecificVariantCompressors WHERE digest = ?", anyDigest.String()); err != nil {
				return void{}, fmt.Errorf("deleting specific variant compressor for digest %q: %w", anyDigest, err)
			}
		} else {
			if _, err := tx.Exec("INSERT OR REPLACE INTO DigestCompressors(digest, compressor) VALUES (?, ?)",
				anyDigest.String(), data.BaseVariantCompressor); err != nil {
				return void{}, fmt.Errorf("recording compressor %q for %q: %w", data.BaseVariantCompressor, anyDigest, err)
			}
		}

		if data.SpecificVariantCompressor != blobinfocache.UnknownCompression {
			if !warned { // Don’t warn twice about the same digest
				prevSVC, found, err := querySingleValue[string](tx, "SELECT specificVariantCompressor FROM DigestSpecificVariantCompressors WHERE digest = ?", anyDigest.String())
				if err != nil {
					return void{}, fmt.Errorf("looking for specific variant compressor of %q", anyDigest)
				}
				if found && data.SpecificVariantCompressor != prevSVC {
					logrus.Warnf("Specific compressor for blob with digest %s previously recorded as %s, now %s", anyDigest, prevSVC, data.SpecificVariantCompressor)
				}
			}
			annotations, err := json.Marshal(data.SpecificVariantAnnotations)
			if err != nil {
				return void{}, err
			}
			if _, err := tx.Exec("INSERT OR REPLACE INTO DigestSpecificVariantCompressors(digest, specificVariantCompressor, specificVariantAnnotations) VALUES (?, ?, ?)",
				anyDigest.String(), data.SpecificVariantCompressor, annotations); err != nil {
				return void{}, fmt.Errorf("recording specific variant compressor %q/%q for %q: %w", data.SpecificVariantCompressor, annotations, anyDigest, err)
			}
		}
		return void{}, nil
	}) // FIXME? Log error (but throttle the log volume on repeated accesses)?
}

// appendReplacementCandidates creates prioritize.CandidateWithTime values for (transport, scope, digest),
// and returns the result of appending them to candidates.
// v2Options is not nil if the caller is CandidateLocations2: this allows including candidates with unknown location, and filters out candidates
// with unknown compression.
func (sqc *cache) appendReplacementCandidates(candidates []prioritize.CandidateWithTime, tx *sql.Tx, transport types.ImageTransport, scope types.BICTransportScope, digest digest.Digest,
	v2Options *blobinfocache.CandidateLocations2Options) ([]prioritize.CandidateWithTime, error) {
	compressionData := blobinfocache.DigestCompressorData{
		BaseVariantCompressor:      blobinfocache.UnknownCompression,
		SpecificVariantCompressor:  blobinfocache.UnknownCompression,
		SpecificVariantAnnotations: nil,
	}
	if v2Options != nil {
		var baseVariantCompressor string
		var specificVariantCompressor sql.NullString
		var annotationBytes []byte
		switch err := tx.QueryRow("SELECT compressor, specificVariantCompressor, specificVariantAnnotations "+
			"FROM DigestCompressors LEFT JOIN DigestSpecificVariantCompressors USING (digest) WHERE digest = ?", digest.String()).
			Scan(&baseVariantCompressor, &specificVariantCompressor, &annotationBytes); {
		case errors.Is(err, sql.ErrNoRows): // Do nothing
		case err != nil:
			return nil, fmt.Errorf("scanning compressor data: %w", err)
		default:
			compressionData.BaseVariantCompressor = baseVariantCompressor
			if specificVariantCompressor.Valid && annotationBytes != nil {
				compressionData.SpecificVariantCompressor = specificVariantCompressor.String
				if err := json.Unmarshal(annotationBytes, &compressionData.SpecificVariantAnnotations); err != nil {
					return nil, err
				}
			}
		}
	}
	template := prioritize.CandidateTemplateWithCompression(v2Options, digest, compressionData)
	if template == nil {
		return candidates, nil
	}

	rows, err := tx.Query("SELECT location, time FROM KnownLocations "+
		"WHERE transport = ? AND scope = ? AND KnownLocations.digest = ?",
		transport.Name(), scope.Opaque, digest.String())
	if err != nil {
		return nil, fmt.Errorf("looking up candidate locations: %w", err)
	}
	defer rows.Close()

	rowAdded := false
	for rows.Next() {
		var location string
		var time time.Time
		if err := rows.Scan(&location, &time); err != nil {
			return nil, fmt.Errorf("scanning candidate: %w", err)
		}
		candidates = append(candidates, template.CandidateWithLocation(types.BICLocationReference{Opaque: location}, time))
		rowAdded = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating through locations: %w", err)
	}

	if !rowAdded && v2Options != nil {
		candidates = append(candidates, template.CandidateWithUnknownLocation())
	}
	return candidates, nil
}

// CandidateLocations2 returns a prioritized, limited, number of blobs and their locations (if known)
// that could possibly be reused within the specified (transport scope) (if they still
// exist, which is not guaranteed).
func (sqc *cache) CandidateLocations2(transport types.ImageTransport, scope types.BICTransportScope, digest digest.Digest, options blobinfocache.CandidateLocations2Options) []blobinfocache.BICReplacementCandidate2 {
	return sqc.candidateLocations(transport, scope, digest, options.CanSubstitute, &options)
}

// candidateLocations implements CandidateLocations / CandidateLocations2.
// v2Options is not nil if the caller is CandidateLocations2.
func (sqc *cache) candidateLocations(transport types.ImageTransport, scope types.BICTransportScope, primaryDigest digest.Digest, canSubstitute bool,
	v2Options *blobinfocache.CandidateLocations2Options) []blobinfocache.BICReplacementCandidate2 {
	var uncompressedDigest digest.Digest // = ""
	res, err := transaction(sqc, func(tx *sql.Tx) ([]prioritize.CandidateWithTime, error) {
		res := []prioritize.CandidateWithTime{}
		res, err := sqc.appendReplacementCandidates(res, tx, transport, scope, primaryDigest, v2Options)
		if err != nil {
			return nil, err
		}
		if canSubstitute {
			uncompressedDigest, err = sqc.uncompressedDigest(tx, primaryDigest)
			if err != nil {
				return nil, err
			}
			if uncompressedDigest != "" {
				// FIXME? We could integrate this with appendReplacementCandidates into a single join instead of N+1 queries.
				// (In the extreme, we could turn _everything_ this function does into a single query.
				// And going even further, even DestructivelyPrioritizeReplacementCandidates could be turned into SQL.)
				// For now, we prioritize simplicity, and sharing both code and implementation structure with the other cache implementations.
				rows, err := tx.Query("SELECT anyDigest FROM DigestUncompressedPairs WHERE uncompressedDigest = ?", uncompressedDigest.String())
				if err != nil {
					return nil, fmt.Errorf("querying for other digests: %w", err)
				}
				defer rows.Close()
				for rows.Next() {
					var otherDigestString string
					if err := rows.Scan(&otherDigestString); err != nil {
						return nil, fmt.Errorf("scanning other digest: %w", err)
					}
					otherDigest, err := digest.Parse(otherDigestString)
					if err != nil {
						return nil, err
					}
					if otherDigest != primaryDigest && otherDigest != uncompressedDigest {
						res, err = sqc.appendReplacementCandidates(res, tx, transport, scope, otherDigest, v2Options)
						if err != nil {
							return nil, err
						}
					}
				}
				if err := rows.Err(); err != nil {
					return nil, fmt.Errorf("iterating through other digests: %w", err)
				}

				if uncompressedDigest != primaryDigest {
					res, err = sqc.appendReplacementCandidates(res, tx, transport, scope, uncompressedDigest, v2Options)
					if err != nil {
						return nil, err
					}
				}
			}
		}
		return res, nil
	})
	if err != nil {
		return []blobinfocache.BICReplacementCandidate2{} // FIXME? Log err (but throttle the log volume on repeated accesses)?
	}
	return prioritize.DestructivelyPrioritizeReplacementCandidates(res, primaryDigest, uncompressedDigest)

}

// CandidateLocations returns a prioritized, limited, number of blobs and their locations that could possibly be reused
// within the specified (transport scope) (if they still exist, which is not guaranteed).
//
// If !canSubstitute, the returned candidates will match the submitted digest exactly; if canSubstitute,
// data from previous RecordDigestUncompressedPair calls is used to also look up variants of the blob which have the same
// uncompressed digest.
func (sqc *cache) CandidateLocations(transport types.ImageTransport, scope types.BICTransportScope, digest digest.Digest, canSubstitute bool) []types.BICReplacementCandidate {
	return blobinfocache.CandidateLocationsFromV2(sqc.candidateLocations(transport, scope, digest, canSubstitute, nil))
}
