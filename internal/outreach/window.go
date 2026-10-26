package outreach

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// WindowSize is independent of jobs and sending history, in logical pixels.
type WindowSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

func (s WindowSize) Valid() bool {
	return s.Width > 0 && s.Height > 0 && s.Width <= 16384 && s.Height <= 16384
}

func (s *Store) LoadWindowSize() (WindowSize, error) {
	var size WindowSize
	var data string
	err := s.db.QueryRow("SELECT value FROM meta WHERE key='window_size'").Scan(&data)
	if err == sql.ErrNoRows {
		return size, nil
	}
	if err != nil {
		return size, err
	}
	if err = json.Unmarshal([]byte(data), &size); err != nil {
		return WindowSize{}, fmt.Errorf("Could not read saved window size: %w", err)
	}
	if !size.Valid() {
		return WindowSize{}, fmt.Errorf("Saved window size is invalid")
	}
	return size, nil
}

func (s *Store) SaveWindowSize(size WindowSize) error {
	if !size.Valid() {
		return fmt.Errorf("Window size is invalid")
	}
	data, err := json.Marshal(size)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO meta(key,value) VALUES('window_size',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", string(data))
	return err
}
