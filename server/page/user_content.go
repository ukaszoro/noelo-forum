package page

import (
	"fmt"
	"forumapp/session"
	"forumapp/storage"
	"forumapp/tmpl"
	"html/template"
	"log"
	"net/http"
	"strings"
)

func LogoutHandler(ses *session.Sessions) http.Handler {
	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		sessionCookie, err := r.Cookie(session.SessionCookie)
		if err == nil {
			ses.Logout(sessionCookie.Value)
		}

		http.Redirect(w, r, "/active", http.StatusSeeOther)
	})
}

func UserContentPost(ses *session.Sessions, strg *storage.Storage) http.Handler {
	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		switch r.FormValue("type") {
		case "comment":
			CommentAction(ses, strg, w, r)
		case "vote":
			VoteAction(ses, strg, w, r)
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, err := w.Write([]byte("400 bad request"))
			if err != nil {
				log.Println("Failed to write 400 response")
			}
			log.Println("400: Bad Request: Wrong `type` field of incoming request")
			return
		}
	})
}

func UserContentGet(ses *session.Sessions, strg *storage.Storage) http.Handler {
	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		resourceType, err := storage.TypeFromURI(r.URL.Path)
		if err != nil {
			NotFoundHandler(w, r)
			return
		}
		switch resourceType {
		case storage.USER_RESOURCE:
			username := strings.FieldsFunc(r.URL.Path, func(c rune) bool {
				return c == '/'
			})[0]
			renderUserPage(ses, strg, username, w, r)
		case storage.POST_RESOURCE:
			renderPost(ses, strg, r.URL.Path, "", w, r)
		case storage.COMMENT_RESOURCE:
			// TODO: render standalone comments with replies (direct link to comment)
			// renderComment(resourcePath, user, w)
			panic("unimplemented!")
		default:
			NotFoundHandler(w, r)
			return
		}
	})
}

func renderUserPage(
	ses *session.Sessions,
	strg *storage.Storage,
	username string,
	w http.ResponseWriter,
	r *http.Request,
) {
	var page tmpl.UserContentPage[tmpl.UserPage]

	sessionCookie, err := r.Cookie(session.SessionCookie)
	if err == nil {
		page.Username, page.IsLoggedIn = ses.CheckAuth(sessionCookie.Value)
	}

	page.Content = tmpl.UserPage{
		Username:           username,
		LogoutButtonActive: page.Username == username,
		TextPosts:          strg.GetUserArticles(username),
	}

	t := template.Must(template.New("").ParseFiles(
		"../templates/user_page.template",
		"../templates/article_list.template",
		"../templates/page.template",
	))

	err = t.ExecuteTemplate(w, "page.template", page)
	if err != nil {
		log.Println("\"user\" page generation failed:", err)
	}
}

func renderPost(
	ses *session.Sessions,
	strg *storage.Storage,
	uri string,
	status string,
	w http.ResponseWriter,
	r *http.Request,
) {
	var page tmpl.UserContentPage[tmpl.TextPost]

	sessionCookie, err := r.Cookie(session.SessionCookie)
	if err == nil {
		page.Username, page.IsLoggedIn = ses.CheckAuth(sessionCookie.Value)
	}

	content, err := strg.GetPost(uri)
	if err != nil {
		log.Println("\"post\" page generation failed:", err)
	}
	content.TextPostError = status
	page.Content = content

	fns := template.FuncMap{
		"indent": func(comments []tmpl.Comment) []tmpl.Comment {
			for i := range comments {
				comments[i].Indentation += 20
			}
			return comments
		},
	}

	t := template.Must(template.New("").Funcs(fns).ParseFiles(
		"../templates/page.template",
		"../templates/textpost.template",
		"../templates/comments.template",
		"../templates/comment.template",
	))

	err = t.ExecuteTemplate(w, "page.template", page)
	if err != nil {
		log.Println("\"post\" page generation failed:", err)
	}
}

func VoteAction(ses *session.Sessions, strg *storage.Storage, w http.ResponseWriter, r *http.Request) {
	location := r.FormValue("location")
	vote := r.FormValue("vote")

	sessionCookie, err := r.Cookie(session.SessionCookie)
	if err != nil {
		log.Println("Error: auth failed")
		renderPost(ses, strg, location, "auth failed", w, r)
		return
	}
	username, isLoggedIn := ses.CheckAuth(sessionCookie.Value)
	if !isLoggedIn {
		log.Println("Error: not logged in")
		renderPost(ses, strg, location, "not logged in", w, r)
		return
	}
	if username == "" {
		return
	}

	post, err := strg.GetPost(location)
	if strings.Contains(post.Upvotes, username) {
		err := strg.RemoveVote(username, location, post.Upvotes)
		if err != nil {
			return
		}
	} else {
		err := strg.AddVote(username, vote, location)
		if err != nil {
			return
		}
	}

	//TODO: remove the redirect and make some css magic to show the upvote number changing client side
	http.Redirect(w, r, r.Header.Get("Referer"), http.StatusFound)
}

func CommentAction(ses *session.Sessions, strg *storage.Storage, w http.ResponseWriter, r *http.Request) {
	location := r.FormValue("location")
	text := r.FormValue("comment")
	sessionCookie, err := r.Cookie(session.SessionCookie)

	if err != nil {
		log.Println("Error: auth failed")
		renderPost(ses, strg, location, "auth failed", w, r)
		return
	}
	username, isLoggedIn := ses.CheckAuth(sessionCookie.Value)
	if !isLoggedIn {
		log.Println("Error: not logged in")
		renderPost(ses, strg, location, "not logged in", w, r)
		return
	}

	if strings.TrimSpace(text) == "" {
		log.Println("Error while adding comment: text is empty")
		renderPost(ses, strg, location, "Please make sure the text include at least one letter and isn't just empty.", w, r)
		return
	}

	id, err := strg.AddComment(username, text, location)
	if err != nil {
		log.Println("Error: ", err)
		renderPost(ses, strg, location, fmt.Sprint("Error: ", err), w, r)
		return
	}

	err = strg.AddCommentRef(username, location, location, "comments", id)
	if err != nil {
		log.Println("Error: ", err)
		renderPost(ses, strg, location, fmt.Sprint("Error: ", err), w, r)
		return
	}

	http.Redirect(w, r, r.Header.Get("Referer"), http.StatusFound)
}

func ReplyAction(ses *session.Sessions, strg *storage.Storage) http.Handler {
	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		location := r.FormValue("location")
		user_location := r.FormValue("user_location")
		text := r.FormValue("comment")

		sessionCookie, err := r.Cookie(session.SessionCookie)
		if err != nil {
			log.Println("Error: auth failed")
			renderPost(ses, strg, location, "auth failed", w, r)
			return
		}
		username, isLoggedIn := ses.CheckAuth(sessionCookie.Value)
		if !isLoggedIn {
			log.Println("Error: not logged in")
			renderPost(ses, strg, location, "not logged in", w, r)
			return
		}

		if strings.TrimSpace(text) == "" {
			log.Println("Error while adding comment: text is empty")
			renderPost(ses, strg, location, "Please make sure the text include at least one letter and isn't just empty.", w, r)
			return
		}

		id, err := strg.AddComment(username, text, user_location)
		if err != nil {
			log.Println("Error: ", err)
			renderPost(ses, strg, location, fmt.Sprint("Error: ", err), w, r)
			return
		}

		err = strg.AddCommentRef(username, user_location, location, "replies", id)
		if err != nil {
			log.Println("Error: ", err)
			renderPost(ses, strg, location, fmt.Sprint("Error: ", err), w, r)
			return
		}

		http.Redirect(w, r, r.Header.Get("Referer"), http.StatusFound)
	})
}
