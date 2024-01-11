package mediaprovider

import (
	"image"
	"io"
	"strings"
)

type AlbumFilter struct {
	MinYear int
	MaxYear int      // 0 == unset/match any
	Genres  []string // len(0) == unset/match any

	ExcludeFavorited   bool // mut. exc. with ExcludeUnfavorited
	ExcludeUnfavorited bool // mut. exc. with ExcludeFavorited
}

// Returns true if the filter is the nil filter - i.e. matches everything
func (a AlbumFilter) IsNil() bool {
	return a.MinYear == 0 && a.MaxYear == 0 &&
		len(a.Genres) == 0 &&
		!a.ExcludeFavorited && !a.ExcludeUnfavorited
}

func (f AlbumFilter) Matches(album *Album) bool {
	if album == nil {
		return false
	}
	if f.ExcludeFavorited && album.Favorite {
		return false
	}
	if f.ExcludeUnfavorited && !album.Favorite {
		return false
	}
	if y := album.Year; y < f.MinYear || (f.MaxYear > 0 && y > f.MaxYear) {
		return false
	}
	if len(f.Genres) == 0 {
		return true
	}
	return genresMatch(f.Genres, album.Genres)
}

type AlbumIterator interface {
	Next() *Album
}

type TrackIterator interface {
	Next() *Track
}

type RatingFavoriteParameters struct {
	AlbumIDs  []string
	ArtistIDs []string
	TrackIDs  []string
}

type Favorites struct {
	Albums  []*Album
	Artists []*Artist
	Tracks  []*Track
}

type LoginResponse struct {
	Error       error
	IsAuthError bool
}

type Server interface {
	Login(username, password string) LoginResponse
	MediaProvider() MediaProvider
}

type MediaProvider interface {
	SetPrefetchCoverCallback(cb func(coverArtID string))

	GetTrack(trackID string) (*Track, error)

	GetAlbum(albumID string) (*AlbumWithTracks, error)

	GetAlbumInfo(albumID string) (*AlbumInfo, error)

	GetArtist(artistID string) (*ArtistWithAlbums, error)

	GetArtistInfo(artistID string) (*ArtistInfo, error)

	GetPlaylist(playlistID string) (*PlaylistWithTracks, error)

	GetCoverArt(coverArtID string, size int) (image.Image, error)

	AlbumSortOrders() []string

	IterateAlbums(sortOrder string, filter AlbumFilter) AlbumIterator

	IterateTracks(searchQuery string) TrackIterator

	SearchAlbums(searchQuery string, filter AlbumFilter) AlbumIterator

	SearchAll(searchQuery string, maxResults int) ([]*SearchResult, error)

	GetRandomTracks(genre string, count int) ([]*Track, error)

	GetSimilarTracks(artistID string, count int) ([]*Track, error)

	GetArtists() ([]*Artist, error)

	GetGenres() ([]*Genre, error)

	GetFavorites() (Favorites, error)

	GetStreamURL(trackID string, forceRaw bool) (string, error)

	GetTopTracks(artist Artist, count int) ([]*Track, error)

	SetFavorite(params RatingFavoriteParameters, favorite bool) error

	GetPlaylists() ([]*Playlist, error)

	CreatePlaylist(name string, trackIDs []string) error

	CanMakePublicPlaylist() bool

	EditPlaylist(id, name, description string, public bool) error

	AddPlaylistTracks(id string, trackIDsToAdd []string) error

	RemovePlaylistTracks(id string, trackIdxsToRemove []int) error

	ReplacePlaylistTracks(id string, trackIDs []string) error

	DeletePlaylist(id string) error

	// True if the `submission` parameter to TrackEndedPlayback will be respected
	// If false, the begin playback scrobble registers a play count immediately
	// when TrackBeganPlayback is invoked.
	ClientDecidesScrobble() bool

	TrackBeganPlayback(trackID string) error

	TrackEndedPlayback(trackID string, positionSecs int, submission bool) error

	DownloadTrack(trackID string) (io.Reader, error)

	RescanLibrary() error
}

type SupportsStreamOffset interface {
	CanStreamWithOffset() bool

	GetStreamURLWithOffset(trackID string, timeOffsetSeconds int) (string, error)
}

type SupportsRating interface {
	SetRating(params RatingFavoriteParameters, rating int) error
}

type JukeboxProvider interface {
	JukeboxStart() error
	JukeboxStop() error
	JukeboxSeek(idx, seconds int) error
	JukeboxClear() error
	JukeboxSet(trackID string) error
	JukeboxAdd(trackID string) error
	JukeboxRemove(idx int) error
	JukeboxSetVolume(vol int) error
	JukeboxGetStatus() (*JukeboxStatus, error)
}

type JukeboxStatus struct {
	Volume          int
	CurrentTrack    int
	Playing         bool
	PositionSeconds float64
}

func genresMatch(filterGenres, albumGenres []string) bool {
	for _, g1 := range filterGenres {
		for _, g2 := range albumGenres {
			if strings.EqualFold(g1, g2) {
				return true
			}
		}
	}
	return false
}
