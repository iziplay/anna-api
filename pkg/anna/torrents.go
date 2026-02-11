package anna

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

type TorrentsResponse struct {
	DisplayName           string `json:"display_name"`
	URL                   string `json:"url"`
	BTIH                  string `json:"btih"`
	MagnetLink            string `json:"magnet_link"`
	TopLevelGroupName     string `json:"top_level_group_name"` // e.g.: other_aa
	GroupName             string `json:"group_name"`           // e.g.: aa_derived_mirror_metadata
	Obsolete              bool   `json:"obsolete"`
	AddedToTorrentsListAt string `json:"added_to_torrents_list_at"`
}

func FetchTorrentsList() ([]TorrentsResponse, error) {
	resp, err := http.Get("https://" + os.Getenv("ANNA_DOMAIN") + "/dyn/torrents.json")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch torrents: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var torrents []TorrentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&torrents); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return torrents, nil
}

func GetLastMetadataTorrent(torrents []TorrentsResponse) *TorrentsResponse {
	var latestTorrent *TorrentsResponse
	var latestDate string

	for _, t := range torrents {
		if t.GroupName == "aa_derived_mirror_metadata" &&
			t.TopLevelGroupName == "other_aa" &&
			!t.Obsolete {
			if t.AddedToTorrentsListAt > latestDate {
				latestDate = t.AddedToTorrentsListAt
				latestTorrent = &t
			}
		}
	}
	return latestTorrent
}
