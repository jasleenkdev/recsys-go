// internal/store/items.go
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrItemNotFound is returned by GetItem when no item has the given id,
// so handlers can map it to a 404 rather than a 500.
var ErrItemNotFound = errors.New("item not found")

// browsePageMax caps the client-supplied limit on the browse endpoint.
const browsePageMax = 100

// Item is a repository as served by the item-detail and browse
// endpoints. Topics is never nil — an empty repo yields an empty slice.
type Item struct {
	ItemID      int64
	Owner       string
	Name        string
	Description string
	Language    string
	Topics      []string
	Stars       int
	GitHubID    int64
	CreatedAt   string
}

// ReadmeSection is one chunk of a repo's README, in document order.
type ReadmeSection struct {
	ChunkIndex     int
	SectionHeading string
	ChunkText      string
}

// ItemDetail is a single item plus its full README, reassembled from
// the same chunks the RAG path cites.
type ItemDetail struct {
	Item
	Readme []ReadmeSection
}

// BrowsePage is one keyset page of the browse listing. NextCursor is
// empty when the page is the last one.
type BrowsePage struct {
	Items      []Item
	NextCursor string
}

// itemColumns is shared by GetItem and ListItems so the two can never
// drift in what they select or how they scan it. topics comes back as
// JSON because database/sql has no native array type.
const itemColumns = `
	i.id, i.owner, i.title, COALESCE(i.description, ''), COALESCE(i.language, ''),
	array_to_json(i.topics)::text, i.stars, COALESCE(i.github_id, 0),
	to_char(i.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
`

func scanItem(s interface{ Scan(...any) error }) (Item, error) {
	var it Item
	var topicsJSON string
	if err := s.Scan(
		&it.ItemID, &it.Owner, &it.Name, &it.Description, &it.Language,
		&topicsJSON, &it.Stars, &it.GitHubID, &it.CreatedAt,
	); err != nil {
		return Item{}, err
	}
	if err := json.Unmarshal([]byte(topicsJSON), &it.Topics); err != nil {
		return Item{}, fmt.Errorf("parsing topics: %w", err)
	}
	if it.Topics == nil {
		it.Topics = []string{}
	}
	return it, nil
}

// GetItem returns one item with its README sections in document order.
// Returns ErrItemNotFound if the id doesn't exist. A repo with no
// ingested README is not an error — Readme is simply empty.
func GetItem(ctx context.Context, db *sql.DB, itemID int64) (ItemDetail, error) {
	row := db.QueryRowContext(ctx, `SELECT `+itemColumns+` FROM items i WHERE i.id = $1`, itemID)

	item, err := scanItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ItemDetail{}, ErrItemNotFound
	}
	if err != nil {
		return ItemDetail{}, fmt.Errorf("fetching item: %w", err)
	}

	sections, err := getReadmeSections(ctx, db, itemID)
	if err != nil {
		return ItemDetail{}, fmt.Errorf("fetching readme: %w", err)
	}

	return ItemDetail{Item: item, Readme: sections}, nil
}

func getReadmeSections(ctx context.Context, db *sql.DB, itemID int64) ([]ReadmeSection, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT chunk_index, COALESCE(section_heading, ''), chunk_text
		FROM readme_chunks
		WHERE item_id = $1
		ORDER BY chunk_index
	`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sections := []ReadmeSection{}
	for rows.Next() {
		var s ReadmeSection
		if err := rows.Scan(&s.ChunkIndex, &s.SectionHeading, &s.ChunkText); err != nil {
			return nil, err
		}
		sections = append(sections, s)
	}
	return sections, rows.Err()
}

// ListItems returns one keyset page of the catalog ordered by
// (stars DESC, id DESC), optionally filtered to a single language.
//
// Unlike the recommendations endpoint this needs no Redis snapshot: the
// ordering key is stored, not computed, so paging straight off the index
// is both cheaper and stable across requests. The (stars, id) pair
// breaks star ties deterministically, which a bare `stars` key would not.
func ListItems(ctx context.Context, db *sql.DB, language string, after *BrowseKey, limit int) (BrowsePage, error) {
	if limit <= 0 || limit > browsePageMax {
		limit = browsePageMax
	}

	// Fetch one extra row to learn whether a further page exists without
	// paying for a separate COUNT.
	args := []any{limit + 1}
	where := ""

	if language != "" {
		args = append(args, language)
		where += fmt.Sprintf(" AND i.language = $%d", len(args))
	}
	if after != nil {
		args = append(args, after.Stars, after.ItemID)
		where += fmt.Sprintf(" AND (i.stars, i.id) < ($%d, $%d)", len(args)-1, len(args))
	}

	rows, err := db.QueryContext(ctx, `
		SELECT `+itemColumns+`
		FROM items i
		WHERE true`+where+`
		ORDER BY i.stars DESC, i.id DESC
		LIMIT $1
	`, args...)
	if err != nil {
		return BrowsePage{}, fmt.Errorf("listing items: %w", err)
	}
	defer rows.Close()

	items := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return BrowsePage{}, fmt.Errorf("scanning item: %w", err)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return BrowsePage{}, fmt.Errorf("listing items: %w", err)
	}

	var next string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		next = EncodeBrowseCursor(BrowseKey{
			Stars:    last.Stars,
			ItemID:   last.ItemID,
			Language: language,
		})
	}

	return BrowsePage{Items: items, NextCursor: next}, nil
}

// Languages returns the distinct languages present in the catalog, most
// repos first — enough for a browse UI to render its filter control
// without hardcoding the ingest categories.
func Languages(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT language
		FROM items
		WHERE language IS NOT NULL AND language <> ''
		GROUP BY language
		ORDER BY count(*) DESC, language
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	langs := []string{}
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		langs = append(langs, l)
	}
	return langs, rows.Err()
}
