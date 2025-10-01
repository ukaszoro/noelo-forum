package storage

import (
	"errors"
	"fmt"
	"forumapp/tmpl"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type Storage struct {
	mu sync.Mutex
}

func NewStorage() (*Storage, error) {
	p := "../storage/users"
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		return &Storage{}, nil
	}
	if err := os.Mkdir(p, 0755); err != nil {
		return nil, fmt.Errorf("failed to create dir %s: %w", p, err)
	}
	return &Storage{}, nil
}

var (
	ErrRegister        = errors.New("registration error")
	ErrInvalidUserData = fmt.Errorf("invalid user data: %w", ErrRegister)
	ErrUserExists      = fmt.Errorf("user already exists: %w", ErrRegister)

	ErrInvalidURI              = errors.New("invalid URI")
	ErrUnsupportedResourceType = errors.New("unsupported type")
)

func (s *Storage) AddUser(email, username, pass string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	username = strings.TrimSpace(strings.ReplaceAll(username, "/", "∕"))
	if email == "" || pass == "" || username == "" || !strings.Contains(email, "@") {
		return ErrInvalidUserData
	}

	userdir := filepath.Join("../storage/users/", username)
	if _, err := os.Stat(userdir); !os.IsNotExist(err) {
		return ErrUserExists
	}

	hashed, _ := bcrypt.GenerateFromPassword([]byte(pass), 8)

	return createPaths(
		[]string{
			userdir,
			filepath.Join(userdir, "comment"),
			filepath.Join(userdir, "post"),
		},
		map[string]string{
			filepath.Join(userdir, "email"): email,
			filepath.Join(userdir, "pass"):  string(hashed),
		},
	)
}

type ResourceType uint

const (
	INVALID = iota
	POST_RESOURCE
	COMMENT_RESOURCE
	USER_RESOURCE
)

func TypeFromURI(path string) (ResourceType, error) {
	urlparts := strings.FieldsFunc(path, func(c rune) bool {
		return c == '/'
	})

	if len(urlparts) == 1 {
		userdir := filepath.Join("../storage/users/", urlparts[0])
		if _, err := os.Stat(userdir); !os.IsNotExist(err) {
			return USER_RESOURCE, nil
		}
	}

	if len(urlparts) < 2 {
		return INVALID, ErrInvalidURI
	}
	resourceURI := strings.Split(urlparts[1], ":")
	if len(resourceURI) < 2 {
		return INVALID, ErrInvalidURI
	}
	resourceType := resourceURI[0]

	switch resourceType {
	case "post":
		return POST_RESOURCE, nil
	case "comment":
		return POST_RESOURCE, nil
	default:
		return INVALID, ErrUnsupportedResourceType
	}
}

func parseUserResourceURI(path string) (
	user string,
	resourcePath string,
	resourceID string,
	err error,
) {
	urlparts := strings.Split(path, "/")
	if len(urlparts) < 2 {
		err = ErrInvalidURI
		return
	}
	resourceURI := strings.Split(urlparts[2], ":")
	if len(resourceURI) < 2 {
		err = ErrInvalidURI
		return
	}
	user = urlparts[1]
	resourceID = resourceURI[1]
	resourceType := resourceURI[0]

	supportedTypes := []string{"post", "comment"}
	if !slices.Contains(supportedTypes, resourceType) {
		err = ErrUnsupportedResourceType
		return
	}

	resourcePath, err = url.JoinPath(user, resourceType, resourceID)
	resourcePath = "../storage/users/" + resourcePath
	return
}

func (s *Storage) AddPost(username, postName, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	userdir := filepath.Join("../storage/users/", username)
	if _, err := os.Stat(userdir); os.IsNotExist(err) {
		return fmt.Errorf("logged in as non existing user: %w", err)
	}

	postDir, id, err := getNextName(filepath.Join(userdir, "post"))
	if err != nil {
		return fmt.Errorf("failed to get next post id: %w", err)
	}

	err = createPaths(
		[]string{
			postDir,
			filepath.Join(postDir, "comments"),
		},
		map[string]string{
			filepath.Join(postDir, "text"):          text,
			filepath.Join(postDir, "title"):         postName,
			filepath.Join(postDir, "creation_date"): time.Now().Format("2006-01-02 15:04"),
			filepath.Join(postDir, "upvotes"):       "",
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create post paths: %w", err)
	}

	_ = s.updateRecents("/" + username + "/post:" + strconv.Itoa(id))

	return nil
}

func (s *Storage) RemoveVote(username, location string, votes string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, resourcePath, _, err := parseUserResourceURI(location)
	if err != nil {
		return fmt.Errorf("failed to parse vote location: %w", err)
	}

	tmp := strings.ReplaceAll(votes, username+"\n", "")

	err = os.WriteFile(filepath.Join(resourcePath, "upvotes"), []byte(tmp), 0644)
	if err != nil {
		return fmt.Errorf("failed to save upvote location: %w", err)
	}
	return nil
}

func (s *Storage) AddVote(username, vote, location string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, resourcePath, _, err := parseUserResourceURI(location)
	if err != nil {
		return fmt.Errorf("failed to parse vote location: %w", err)
	}

	f, err := os.OpenFile(filepath.Join(resourcePath, "upvotes"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("Failed to open file %s: %w", f.Name(), err)
	}
	if _, err := f.Write([]byte(username + "\n")); err != nil {
		return fmt.Errorf("Failed to write to file %s: %w", f.Name(), err)

	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("Failed to close file %s: %w", f.Name(), err)
	}

	return nil
}

func (s *Storage) AddComment(username, text, location string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	userdir := filepath.Join("../storage/users/", username)
	if _, err := os.Stat(userdir); os.IsNotExist(err) {
		return 0, fmt.Errorf("logged in as non existing user: %w", err)
	}

	commentDir, id, err := getNextName(filepath.Join(userdir, "comment"))
	if err != nil {
		return 0, fmt.Errorf("failed to get next comment id: %w", err)
	}

	err = createPaths(
		[]string{
			commentDir,
			filepath.Join(commentDir, "replies"),
		},
		map[string]string{
			filepath.Join(commentDir, "text"):          text,
			filepath.Join(commentDir, "creation_date"): time.Now().Format("2006-01-02 15:04"),
			filepath.Join(commentDir, "location"):      location,
		},
	)
	if err != nil {
		return 0, fmt.Errorf("failed to create comment paths: %w", err)
	}

	return id, nil
}

func (s *Storage) AddCommentRef(username, location, root_location, dir string, id int) error {
	_, resourcePath, _, err := parseUserResourceURI(location)
	if err != nil {
		return fmt.Errorf("failed to parse comment location: %w", err)
	}

	commentRef, _, err := getNextName(filepath.Join(resourcePath, dir))
	if err != nil {
		return fmt.Errorf("failed to get next comment reference: %w", err)
	}

	refContent := fmt.Sprintf("/%s/comment:%d", username, id)
	if err := createPaths(nil, map[string]string{
		commentRef: refContent,
	}); err != nil {
		return fmt.Errorf("failed to create comment reference: %w", err)
	}

	_ = s.updateRecents(root_location)

	return nil
}

func getNextName(basePath string) (string, int, error) {
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return "", 0, err
	}

	maxNum := -1
	for _, entry := range entries {
		num, err := strconv.Atoi(entry.Name())
		if err == nil && num > maxNum {
			maxNum = num
		}
	}

	return filepath.Join(basePath, strconv.Itoa(maxNum+1)), maxNum + 1, nil
}

func (s *Storage) GetPost(uri string) (tmpl.TextPost, error) {
	user, resourcePath, _, err := parseUserResourceURI(uri)
	if err != nil {
		return tmpl.TextPost{}, fmt.Errorf("failed to parse post location: %w", err)
	}
	title, err := os.ReadFile(filepath.Join(resourcePath, "title"))
	if err != nil {
		return tmpl.TextPost{}, fmt.Errorf("failed to get post title: %w", err)
	}
	text, err := os.ReadFile(filepath.Join(resourcePath, "text"))
	if err != nil {
		return tmpl.TextPost{}, fmt.Errorf("failed to get post text: %w", err)
	}
	creation_date, err := os.ReadFile(filepath.Join(resourcePath, "creation_date"))
	if err != nil {
		return tmpl.TextPost{}, fmt.Errorf("failed to get creation date: %w", err)
	}
	comments, err := getReplies(resourcePath, "comments")
	if err != nil && !os.IsNotExist(err) {
		return tmpl.TextPost{}, fmt.Errorf("failed to get post comments: %w", err)
	}
	upvotes, err := os.ReadFile(filepath.Join(resourcePath, "upvotes"))
	if err != nil {
		upvotes = []byte("0")
	}

	return tmpl.TextPost{
		Location:     uri,
		Title:        string(title),
		Text:         string(text),
		Author:       user,
		CreationDate: string(creation_date),
		Comments:     comments,
		Upvotes:      string(upvotes),
	}, nil
}

func parseComment(resourcePath string, user string, id string) (tmpl.Comment, bool) {
	creation_date, err := os.ReadFile(filepath.Join(resourcePath, "creation_date"))
	if err != nil {
		return tmpl.Comment{}, false
	}
	location, err := os.ReadFile(filepath.Join(resourcePath, "location"))
	if err != nil {
		return tmpl.Comment{}, false
	}
	text, err := os.ReadFile(filepath.Join(resourcePath, "text"))
	if err != nil {
		return tmpl.Comment{}, false
	}
	replies, err := getReplies(resourcePath, "replies")
	if err != nil {
		return tmpl.Comment{}, false
	}
	userLocation := fmt.Sprintf("/%s/comment:%s", user, id)

	return tmpl.Comment{
		Author:       user,
		CreationDate: string(creation_date),
		Location:     string(location),
		UserLocation: userLocation,
		Text:         string(text),
		Replies:      replies,
	}, true
}

func getReplies(postPath, commentDirName string) ([]tmpl.Comment, error) {
	var comments []tmpl.Comment
	commentDir := filepath.Join(postPath, commentDirName)
	files, err := os.ReadDir(commentDir)
	if err != nil {
		return []tmpl.Comment{}, err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		commentURI, err := os.ReadFile(filepath.Join(commentDir, file.Name()))
		if err != nil {
			continue
		}
		user, resourcePath, id, err := parseUserResourceURI(strings.TrimSpace(string(commentURI)))
		if err != nil {
			continue
		}
		comment, ok := parseComment(resourcePath, user, id)
		if !ok {
			continue
		}
		comments = append(comments, comment)
	}
	return comments, nil
}

func (s *Storage) GetRecentlyActive(count uint) []tmpl.ArticleItem {
	recents := "../storage/recents"
	URIs, err := os.ReadFile(recents)
	if err != nil {
		return []tmpl.ArticleItem{}
	}

	dedupedURIs := []string{}
	for _, v := range strings.Split(string(URIs), "\n") {
		if slices.Contains(dedupedURIs, v) {
			continue
		}
		dedupedURIs = append(dedupedURIs, v)
	}

	var articles []tmpl.ArticleItem
	for _, v := range dedupedURIs {
		user, resourcePath, _, err := parseUserResourceURI(v)
		if err != nil {
			continue
		}
		title, err := os.ReadFile(filepath.Join(resourcePath, "title"))
		if err != nil {
			continue
		}
		creation_date, err := os.ReadFile(filepath.Join(resourcePath, "creation_date"))
		if err != nil {
			continue
		}
		articles = append(articles, tmpl.ArticleItem{
			Title:        string(title),
			Author:       user,
			CreationDate: string(creation_date),
			PostLink:     "/u" + v,
		})
	}

	return articles
}

func (s *Storage) GetUserArticles(username string) []tmpl.ArticleItem {
	var articles []tmpl.ArticleItem

	userPosts := filepath.Join("../storage/users", username, "post")
	userPostsDir, err := os.ReadDir(userPosts)
	if err != nil {
		return []tmpl.ArticleItem{}
	}

	for _, v := range userPostsDir {
		title, err := os.ReadFile(filepath.Join(userPosts, v.Name(), "title"))
		if err != nil {
			continue
		}
		creation_date, err := os.ReadFile(filepath.Join(userPosts, v.Name(), "creation_date"))
		if err != nil {
			continue
		}
		articles = append(articles, tmpl.ArticleItem{
			Title:        string(title),
			Author:       username,
			CreationDate: string(creation_date),
			PostLink:     "/u/" + username + "/post:" + v.Name(),
		})
	}

	return articles
}

func (s *Storage) GetAllArticles() []tmpl.ArticleItem {
	var articles []tmpl.ArticleItem
	dirPath := "../storage/users"
	users, err := os.ReadDir(dirPath)
	if err != nil {
		return articles
	}

	for _, userData := range users {
		if !userData.IsDir() {
			continue
		}
		userArticles := s.GetUserArticles(userData.Name())
		if len(userArticles) == 0 {
			continue
		}
		articles = append(articles, userArticles...)
	}

	return articles
}

// Only use when `mu` is locked
func (s *Storage) updateRecents(postURI string) error {
	// Read the original file
	content, err := os.ReadFile("../storage/recents")
	if err != nil {
		content = []byte{}
	}

	// Open file for writing (truncate)
	file, err := os.Create("../storage/recents")
	if err != nil {
		return err
	}

	// Write the new line followed by the original content
	_, err = file.WriteString(postURI + "\n" + string(content))
	_ = file.Close()
	return err
}
