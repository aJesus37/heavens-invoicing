package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jesus/invoice-app/internal/i18n"
	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/repo"
)

type productForm struct {
	Name        string
	Description string
	Price       string
	Active      bool
	Error       string
	Action      string
	Editing     bool
	ID          string
}

type produtosData struct {
	Products   []*model.Product
	Created    bool
	Q          string
	Page       int
	Total      int
	TotalPages int
	HasPrev    bool
	HasNext    bool
	PrevURL    string
	NextURL    string
	Pages      []pageLink
}

func productPageURL(page int, q string) string {
	v := url.Values{}
	if q != "" {
		v.Set("q", q)
	}
	if page > 1 {
		v.Set("page", strconv.Itoa(page))
	}
	if len(v) == 0 {
		return "/products"
	}
	return "/products?" + v.Encode()
}

func (h *Handlers) listProducts(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	page := 1
	if s := r.URL.Query().Get("page"); s != "" {
		if p, err := strconv.Atoi(s); err == nil && p > 0 {
			page = p
		}
	}
	products, total, err := h.repos.Products.ListPaginated(r.Context(), page, q)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	created := r.URL.Query().Get("created") == "1"
	perPage := repo.ProductPageSize
	totalPages := (total + perPage - 1) / perPage
	if totalPages == 0 {
		totalPages = 1
	}
	hasPrev := page > 1
	hasNext := page < totalPages
	var prevURL, nextURL string
	if hasPrev {
		prevURL = productPageURL(page-1, q)
	}
	if hasNext {
		nextURL = productPageURL(page+1, q)
	}
	pages := make([]pageLink, 0, totalPages)
	for i := 1; i <= totalPages; i++ {
		pages = append(pages, pageLink{Num: i, URL: productPageURL(i, q), Active: i == page})
	}
	h.renderPage(w, r, http.StatusOK, "products.html", i18n.T(lang, "products.title"), lang, produtosData{
		Products:   products,
		Created:    created,
		Q:          q,
		Page:       page,
		Total:      total,
		TotalPages: totalPages,
		HasPrev:    hasPrev,
		HasNext:    hasNext,
		PrevURL:    prevURL,
		NextURL:    nextURL,
		Pages:      pages,
	})
}

func (h *Handlers) newProductForm(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	h.renderPage(w, r, http.StatusOK, "product_form.html", i18n.T(lang, "products.new"), lang, &productForm{Action: "/products/new"})
}

func formToProduct(r *http.Request) (*model.Product, productForm) {
	f := productForm{
		Name:        r.FormValue("name"),
		Description: r.FormValue("description"),
		Price:       r.FormValue("unit_price"),
		Active:      r.FormValue("active") == "on",
	}
	return &model.Product{Name: f.Name, Description: f.Description}, f
}

func (h *Handlers) createProduct(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	if err := r.ParseForm(); err != nil {
		failBadRequest(w, lang)
		return
	}
	p, f := formToProduct(r)
	f.Action = "/products/new"

	cents, err := parseReais(lang, f.Price)
	if err != nil {
		f.Error = i18n.T(lang, "error.unit_price", err.Error())
		h.renderPage(w, r, http.StatusBadRequest, "product_form.html", i18n.T(lang, "products.new"), lang, &f)
		return
	}
	if strings.TrimSpace(p.Name) == "" {
		f.Error = i18n.T(lang, "error.name_required")
		h.renderPage(w, r, http.StatusBadRequest, "product_form.html", i18n.T(lang, "products.new"), lang, &f)
		return
	}
	p.UnitPrice = cents
	if _, err := h.repos.Products.Create(r.Context(), p); err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	http.Redirect(w, r, "/products?created=1", http.StatusSeeOther)
}

func (h *Handlers) editProductForm(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	p, err := h.repos.Products.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	f := &productForm{
		Name:        p.Name,
		Description: p.Description,
		Price:       formatReais(p.UnitPrice),
		Active:      p.Active,
		Action:      "/products/" + p.ID + "/edit",
		Editing:     true,
		ID:          p.ID,
	}
	h.renderPage(w, r, http.StatusOK, "product_form.html", i18n.T(lang, "products.edit_title"), lang, f)
}

func (h *Handlers) updateProduct(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	id := r.PathValue("id")
	p, err := h.repos.Products.Get(r.Context(), id)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		failBadRequest(w, lang)
		return
	}
	_, f := formToProduct(r)
	cents, err := parseReais(lang, f.Price)
	if err != nil {
		f.Error = i18n.T(lang, "error.unit_price", err.Error())
		f.Action = "/products/" + id + "/edit"
		f.Editing = true
		f.ID = id
		h.renderPage(w, r, http.StatusBadRequest, "product_form.html", i18n.T(lang, "products.edit_title"), lang, &f)
		return
	}
	if strings.TrimSpace(f.Name) == "" {
		f.Error = i18n.T(lang, "error.name_required")
		f.Action = "/products/" + id + "/edit"
		f.Editing = true
		f.ID = id
		h.renderPage(w, r, http.StatusBadRequest, "product_form.html", i18n.T(lang, "products.edit_title"), lang, &f)
		return
	}
	p.Name = strings.TrimSpace(f.Name)
	p.Description = f.Description
	p.UnitPrice = cents
	p.Active = f.Active
	if err := h.repos.Products.Update(r.Context(), p); err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	http.Redirect(w, r, "/products", http.StatusSeeOther)
}

func (h *Handlers) deleteProduct(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	id := r.PathValue("id")
	if err := h.repos.Products.Delete(r.Context(), id); err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	http.Redirect(w, r, "/products", http.StatusSeeOther)
}
