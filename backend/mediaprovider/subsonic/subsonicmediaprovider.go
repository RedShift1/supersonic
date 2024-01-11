package subsonic

import (
	"errors"
	"image"
	"io"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dweymouth/go-subsonic/subsonic"
	"github.com/dweymouth/supersonic/backend/mediaprovider"
	"github.com/dweymouth/supersonic/sharedutil"
)

const cacheValidDurationSeconds = 60

type subsonicMediaProvider struct {
	client          *subsonic.Client
	prefetchCoverCB func(coverArtID string)

	genresCached   []*mediaprovider.Genre
	genresCachedAt int64 // unix

	playlistsCached   []*mediaprovider.Playlist
	playlistsCachedAt int64 // unix
}

// assert compliance with interfaces
var (
	_ mediaprovider.MediaProvider        = (*subsonicMediaProvider)(nil)
	_ mediaprovider.SupportsRating       = (*subsonicMediaProvider)(nil)
	_ mediaprovider.SupportsStreamOffset = (*subsonicMediaProvider)(nil)
)

func SubsonicMediaProvider(subsonicClient *subsonic.Client) mediaprovider.MediaProvider {
	return &subsonicMediaProvider{client: subsonicClient}
}

func (s *subsonicMediaProvider) SetPrefetchCoverCallback(cb func(coverArtID string)) {
	s.prefetchCoverCB = cb
}

func (s *subsonicMediaProvider) CreatePlaylist(name string, trackIDs []string) error {
	return s.client.CreatePlaylistWithTracks(trackIDs, map[string]string{"name": name})
}

func (s *subsonicMediaProvider) DeletePlaylist(id string) error {
	return s.client.DeletePlaylist(id)
}

func (s *subsonicMediaProvider) CanMakePublicPlaylist() bool {
	return true
}

func (s *subsonicMediaProvider) EditPlaylist(id, name, description string, public bool) error {
	return s.client.UpdatePlaylist(id, map[string]string{
		"name":    name,
		"comment": description,
		"public":  strconv.FormatBool(public),
	})
}

func (s *subsonicMediaProvider) AddPlaylistTracks(id string, trackIDsToAdd []string) error {
	return s.client.UpdatePlaylistTracks(id, trackIDsToAdd, nil)
}

func (s *subsonicMediaProvider) RemovePlaylistTracks(id string, removeIdxs []int) error {
	return s.client.UpdatePlaylistTracks(id, nil, removeIdxs)
}

func (s *subsonicMediaProvider) GetTrack(trackID string) (*mediaprovider.Track, error) {
	tr, err := s.client.GetSong(trackID)
	if err != nil {
		return nil, err
	}
	return toTrack(tr), nil
}

func (s *subsonicMediaProvider) GetAlbum(albumID string) (*mediaprovider.AlbumWithTracks, error) {
	al, err := s.client.GetAlbum(albumID)
	if err != nil {
		return nil, err
	}
	album := &mediaprovider.AlbumWithTracks{
		Tracks: sharedutil.MapSlice(al.Song, toTrack),
	}
	fillAlbum(al, &album.Album)
	return album, nil
}

func (s *subsonicMediaProvider) GetAlbumInfo(albumID string) (*mediaprovider.AlbumInfo, error) {
	al, err := s.client.GetAlbumInfo(albumID)
	if err != nil {
		return nil, err
	}
	album := &mediaprovider.AlbumInfo{
		Notes:         al.Notes,
		LastFmUrl:     al.LastFmUrl,
		MusicBrainzID: al.MusicBrainzID,
	}
	return album, nil
}

func (s *subsonicMediaProvider) GetArtist(artistID string) (*mediaprovider.ArtistWithAlbums, error) {
	ar, err := s.client.GetArtist(artistID)
	if err != nil {
		return nil, err
	}
	return &mediaprovider.ArtistWithAlbums{
		Artist: mediaprovider.Artist{
			ID:         ar.ID,
			Name:       ar.Name,
			Favorite:   !ar.Starred.IsZero(),
			AlbumCount: ar.AlbumCount,
		},
		Albums: sharedutil.MapSlice(ar.Album, toAlbum),
	}, nil
}

func (s *subsonicMediaProvider) GetArtistInfo(artistID string) (*mediaprovider.ArtistInfo, error) {
	info, err := s.client.GetArtistInfo2(artistID, map[string]string{})
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, errors.New("server returned empty artist info")
	}
	return &mediaprovider.ArtistInfo{
		Biography:      info.Biography,
		LastFMUrl:      info.LastFmUrl,
		ImageURL:       info.LargeImageUrl,
		SimilarArtists: sharedutil.MapSlice(info.SimilarArtist, toArtistFromID3),
	}, nil
}

func (s *subsonicMediaProvider) GetArtists() ([]*mediaprovider.Artist, error) {
	idxs, err := s.client.GetArtists(map[string]string{})
	if err != nil {
		return nil, err
	}
	var artists []*mediaprovider.Artist
	for _, idx := range idxs.Index {
		for _, ar := range idx.Artist {
			artists = append(artists, toArtistFromID3(ar))
		}
	}
	return artists, nil
}

func (s *subsonicMediaProvider) GetCoverArt(id string, size int) (image.Image, error) {
	params := map[string]string{}
	if size > 0 {
		params["size"] = strconv.Itoa(size)
	}
	return s.client.GetCoverArt(id, params)
}

func (s *subsonicMediaProvider) GetFavorites() (mediaprovider.Favorites, error) {
	fav, err := s.client.GetStarred2(map[string]string{})
	if err != nil {
		return mediaprovider.Favorites{}, err
	}
	return mediaprovider.Favorites{
		Albums:  sharedutil.MapSlice(fav.Album, toAlbum),
		Artists: sharedutil.MapSlice(fav.Artist, toArtistFromID3),
		Tracks:  sharedutil.MapSlice(fav.Song, toTrack),
	}, nil
}

func (s *subsonicMediaProvider) GetGenres() ([]*mediaprovider.Genre, error) {
	if s.genresCached != nil && time.Now().Unix()-s.genresCachedAt < cacheValidDurationSeconds {
		return s.genresCached, nil
	}

	g, err := s.client.GetGenres()
	if err != nil {
		return nil, err
	}
	s.genresCached = sharedutil.MapSlice(g, func(g *subsonic.Genre) *mediaprovider.Genre {
		return &mediaprovider.Genre{
			Name:       g.Name,
			AlbumCount: g.AlbumCount,
			TrackCount: g.SongCount,
		}
	})
	s.genresCachedAt = time.Now().Unix()
	return s.genresCached, nil
}

func (s *subsonicMediaProvider) GetPlaylist(playlistID string) (*mediaprovider.PlaylistWithTracks, error) {
	pl, err := s.client.GetPlaylist(playlistID)
	if err != nil {
		return nil, err
	}
	playlist := &mediaprovider.PlaylistWithTracks{
		Tracks: sharedutil.MapSlice(pl.Entry, toTrack),
	}
	fillPlaylist(pl, &playlist.Playlist)
	return playlist, nil
}

func (s *subsonicMediaProvider) GetPlaylists() ([]*mediaprovider.Playlist, error) {
	if s.playlistsCached != nil && time.Now().Unix()-s.playlistsCachedAt < cacheValidDurationSeconds {
		return s.playlistsCached, nil
	}

	pl, err := s.client.GetPlaylists(map[string]string{})
	if err != nil {
		return nil, err
	}
	s.playlistsCached = sharedutil.MapSlice(pl, toPlaylist)
	s.playlistsCachedAt = time.Now().Unix()
	return s.playlistsCached, nil
}

func (s *subsonicMediaProvider) GetRandomTracks(genreName string, count int) ([]*mediaprovider.Track, error) {
	opts := map[string]string{"size": strconv.Itoa(count)}
	if genreName != "" {
		opts["genre"] = genreName
	}
	tr, err := s.client.GetRandomSongs(opts)
	if err != nil {
		return nil, err
	}
	return sharedutil.MapSlice(tr, toTrack), nil
}

func (s *subsonicMediaProvider) GetSimilarTracks(artistID string, count int) ([]*mediaprovider.Track, error) {
	tr, err := s.client.GetSimilarSongs2(artistID, map[string]string{"count": strconv.Itoa(count)})
	if err != nil {
		return nil, err
	}
	return sharedutil.MapSlice(tr, toTrack), nil
}

func (s *subsonicMediaProvider) GetStreamURL(trackID string, forceRaw bool) (string, error) {
	m := make(map[string]string)
	if forceRaw {
		m["format"] = "raw"
	}
	u, err := s.client.GetStreamURL(trackID, m)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (s *subsonicMediaProvider) GetTopTracks(artist mediaprovider.Artist, count int) ([]*mediaprovider.Track, error) {
	params := map[string]string{}
	if count > 0 {
		params["count"] = strconv.Itoa(count)
	}
	tr, err := s.client.GetTopSongs(artist.Name, params)
	if err != nil {
		return nil, err
	}
	return sharedutil.MapSlice(tr, toTrack), nil
}

func (s *subsonicMediaProvider) ReplacePlaylistTracks(playlistID string, trackIDs []string) error {
	return s.client.CreatePlaylistWithTracks(trackIDs, map[string]string{"playlistId": playlistID})
}

func (s *subsonicMediaProvider) ClientDecidesScrobble() bool { return true }

func (s *subsonicMediaProvider) TrackBeganPlayback(trackID string) error {
	return s.client.Scrobble(trackID, map[string]string{
		"time":       strconv.FormatInt(time.Now().UnixMilli(), 10),
		"submission": "false"})
}

func (s *subsonicMediaProvider) TrackEndedPlayback(trackID string, _ int, submission bool) error {
	if !submission {
		return nil
	}
	return s.client.Scrobble(trackID, map[string]string{
		"time":       strconv.FormatInt(time.Now().UnixMilli(), 10),
		"submission": "true"})
}

func (s *subsonicMediaProvider) SetFavorite(params mediaprovider.RatingFavoriteParameters, favorite bool) error {
	subParams := subsonic.StarParameters{
		AlbumIDs:  params.AlbumIDs,
		ArtistIDs: params.ArtistIDs,
		SongIDs:   params.TrackIDs,
	}
	if favorite {
		return s.client.Star(subParams)
	}
	return s.client.Unstar(subParams)
}

func (s *subsonicMediaProvider) SetRating(params mediaprovider.RatingFavoriteParameters, rating int) error {
	// Subsonic doesn't allow bulk setting ratings.
	// To not overwhelm the server with requests, set rating for
	// only 5 tracks at a time concurrently
	batchSize := 5
	var err error
	batchSetRating := func(offs int, wg *sync.WaitGroup) {
		for i := 0; i < batchSize && offs+i < len(params.TrackIDs); i++ {
			wg.Add(1)
			go func(idx int) {
				newErr := s.client.SetRating(params.TrackIDs[idx], rating)
				if err == nil && newErr != nil {
					err = newErr
				}
				wg.Done()
			}(offs + i)
		}
	}

	numBatches := int(math.Ceil(float64(len(params.TrackIDs)) / float64(batchSize)))
	for i := 0; i < numBatches; i++ {
		var wg sync.WaitGroup
		batchSetRating(i*batchSize, &wg)
		wg.Wait()
	}

	return err
}

func (s *subsonicMediaProvider) DownloadTrack(trackID string) (io.Reader, error) {
	return s.client.Download(trackID)
}

func (s *subsonicMediaProvider) RescanLibrary() error {
	_, err := s.client.StartScan()
	return err
}

func (s *subsonicMediaProvider) CanStreamWithOffset() bool {
	extensions, err := s.client.GetOpenSubsonicExtensions()
	if err != nil {
		return false
	}
	log.Printf("OpenSubsonic extensions: %v", extensions)
	for _, ext := range extensions {
		if ext.Name == subsonic.TranscodeOffset {
			return true
		}
	}
	return false
}

func (s *subsonicMediaProvider) GetStreamURLWithOffset(trackID string, offsetSeconds int) (string, error) {
	u, err := s.client.GetStreamURL(trackID, map[string]string{"timeOffset": strconv.Itoa(offsetSeconds)})
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func toTrack(ch *subsonic.Child) *mediaprovider.Track {
	if ch == nil {
		return nil
	}
	var artistNames, artistIDs []string
	if len(ch.Artists) > 0 {
		// OpenSubsonic extension
		for _, a := range ch.Artists {
			artistIDs = append(artistIDs, a.ID)
			artistNames = append(artistNames, a.Name)
		}
	} else {
		artistNames = append(artistNames, ch.Artist)
		artistIDs = append(artistIDs, ch.ArtistID)
	}

	return &mediaprovider.Track{
		ID:          ch.ID,
		CoverArtID:  ch.CoverArt,
		ParentID:    ch.Parent,
		Name:        ch.Title,
		Duration:    ch.Duration,
		TrackNumber: ch.Track,
		DiscNumber:  ch.DiscNumber,
		Genre:       ch.Genre,
		ArtistIDs:   artistIDs,
		ArtistNames: artistNames,
		Album:       ch.Album,
		AlbumID:     ch.AlbumID,
		Year:        ch.Year,
		Rating:      ch.UserRating,
		Favorite:    !ch.Starred.IsZero(),
		PlayCount:   int(ch.PlayCount),
		FilePath:    ch.Path,
		Size:        ch.Size,
		BitRate:     ch.BitRate,
		Comment:     ch.Comment,
	}
}

func toAlbum(al *subsonic.AlbumID3) *mediaprovider.Album {
	if al == nil {
		return nil
	}
	album := &mediaprovider.Album{}
	fillAlbum(al, album)
	return album
}

func fillAlbum(subAlbum *subsonic.AlbumID3, album *mediaprovider.Album) {
	var artistNames, artistIDs []string
	if len(subAlbum.Artists) > 0 {
		// OpenSubsonic extension
		for _, a := range subAlbum.Artists {
			artistIDs = append(artistIDs, a.ID)
			artistNames = append(artistNames, a.Name)
		}
	} else {
		artistNames = append(artistNames, subAlbum.Artist)
		artistIDs = append(artistIDs, subAlbum.ArtistID)
	}

	var genres []string
	if len(subAlbum.Genres) > 0 {
		// OpenSubsonic extension
		for _, g := range subAlbum.Genres {
			genres = append(genres, g.Name)
		}
	} else {
		genres = append(genres, subAlbum.Genre)
	}

	album.ID = subAlbum.ID
	album.CoverArtID = subAlbum.CoverArt
	album.Name = subAlbum.Name
	album.Duration = subAlbum.Duration
	album.ArtistIDs = artistIDs
	album.ArtistNames = artistNames
	album.Year = subAlbum.Year
	album.TrackCount = subAlbum.SongCount
	album.Genres = genres
	album.Favorite = !subAlbum.Starred.IsZero()
	album.ReleaseTypes = normalizeReleaseTypes(subAlbum.ReleaseTypes)
	if subAlbum.IsCompilation {
		album.ReleaseTypes |= mediaprovider.ReleaseTypeCompilation
	}
}

func normalizeReleaseTypes(releaseTypes []string) mediaprovider.ReleaseTypes {
	var mpReleaseTypes mediaprovider.ReleaseTypes
	for _, t := range releaseTypes {
		switch strings.ToLower(strings.ReplaceAll(t, " ", "")) {
		case "album":
			mpReleaseTypes |= mediaprovider.ReleaseTypeAlbum
		case "audiobook":
			mpReleaseTypes |= mediaprovider.ReleaseTypeAudiobook
		case "audiodrama":
			mpReleaseTypes |= mediaprovider.ReleaseTypeAudioDrama
		case "broadcast":
			mpReleaseTypes |= mediaprovider.ReleaseTypeBroadcast
		case "compilation":
			mpReleaseTypes |= mediaprovider.ReleaseTypeCompilation
		case "demo":
			mpReleaseTypes |= mediaprovider.ReleaseTypeDemo
		case "djmix":
			mpReleaseTypes |= mediaprovider.ReleaseTypeDJMix
		case "ep":
			mpReleaseTypes |= mediaprovider.ReleaseTypeEP
		case "fieldrecording":
			mpReleaseTypes |= mediaprovider.ReleaseTypeFieldRecording
		case "interview":
			mpReleaseTypes |= mediaprovider.ReleaseTypeInterview
		case "live":
			mpReleaseTypes |= mediaprovider.ReleaseTypeLive
		case "mixtape":
			mpReleaseTypes |= mediaprovider.ReleaseTypeMixtape
		case "remix":
			mpReleaseTypes |= mediaprovider.ReleaseTypeRemix
		case "single":
			mpReleaseTypes |= mediaprovider.ReleaseTypeSingle
		case "soundtrack":
			mpReleaseTypes |= mediaprovider.ReleaseTypeSoundtrack
		case "spokenword":
			mpReleaseTypes |= mediaprovider.ReleaseTypeSpokenWord
		}
	}
	if mpReleaseTypes == 0 {
		return mediaprovider.ReleaseTypeAlbum
	}
	return mpReleaseTypes
}

func toArtistFromID3(ar *subsonic.ArtistID3) *mediaprovider.Artist {
	if ar == nil {
		return nil
	}
	return &mediaprovider.Artist{
		ID:         ar.ID,
		CoverArtID: ar.CoverArt,
		Name:       ar.Name,
		Favorite:   !ar.Starred.IsZero(),
		AlbumCount: ar.AlbumCount,
	}
}

func toPlaylist(pl *subsonic.Playlist) *mediaprovider.Playlist {
	if pl == nil {
		return nil
	}
	playlist := &mediaprovider.Playlist{}
	fillPlaylist(pl, playlist)
	return playlist
}

func fillPlaylist(pl *subsonic.Playlist, playlist *mediaprovider.Playlist) {
	playlist.Name = pl.Name
	playlist.ID = pl.ID
	playlist.CoverArtID = pl.CoverArt
	playlist.Description = pl.Comment
	playlist.Owner = pl.Owner
	playlist.Public = pl.Public
	playlist.TrackCount = pl.SongCount
	playlist.Duration = pl.Duration
}
