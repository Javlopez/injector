package injector_test

import (
	"database/sql"
	"log/slog"
	"testing"

	"github.com/Javlopez/injector"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file is a real-world bench for the recursive auto-wiring. It models a
// generic repo → service → handler application with the patterns a production
// composition root relies on:
//
//   - shared leaf singletons (*sql.DB, *slog.Logger) injected into many constructors
//   - repositories whose constructors RETURN AN INTERFACE (exact-match path)
//   - a service that fans in several sibling services plus a repo
//   - a three-level repo → service → handler chain
//
// The point: replace a long block of hand-ordered manual wiring with N
// order-independent Inject calls plus one resolve, and prove the whole graph
// still shares singletons.

// --- repository interfaces ---

type UserRepo interface{ userMarker() }
type ClientRepo interface{ clientMarker() }
type AdminRepo interface{ adminMarker() }
type ReportRepo interface{ reportMarker() }

// --- concrete implementations returned by the constructors ---

type sqlUserRepo struct{ db *sql.DB }

func (*sqlUserRepo) userMarker() {}

type sqlClientRepo struct{ db *sql.DB }

func (*sqlClientRepo) clientMarker() {}

type sqlAdminRepo struct{ db *sql.DB }

func (*sqlAdminRepo) adminMarker() {}

type sqlReportRepo struct{ db *sql.DB }

func (*sqlReportRepo) reportMarker() {}

// Constructors return the INTERFACE, the common repository idiom.
func newUserRepo(db *sql.DB) UserRepo     { return &sqlUserRepo{db: db} }
func newClientRepo(db *sql.DB) ClientRepo { return &sqlClientRepo{db: db} }
func newAdminRepo(db *sql.DB) AdminRepo   { return &sqlAdminRepo{db: db} }
func newReportRepo(db *sql.DB) ReportRepo { return &sqlReportRepo{db: db} }

// --- services (return concrete *Service, take interface deps + *slog.Logger) ---

type UserService struct {
	repo   UserRepo
	logger *slog.Logger
}

func newUserService(r UserRepo, l *slog.Logger) *UserService {
	return &UserService{repo: r, logger: l}
}

type ClientService struct {
	repo   ClientRepo
	logger *slog.Logger
}

func newClientService(r ClientRepo, l *slog.Logger) *ClientService {
	return &ClientService{repo: r, logger: l}
}

type AdminService struct {
	repo   AdminRepo
	logger *slog.Logger
}

func newAdminService(r AdminRepo, l *slog.Logger) *AdminService {
	return &AdminService{repo: r, logger: l}
}

// ReportService fans in three sibling services plus its own repo — the gnarliest
// node in the graph.
type ReportService struct {
	users   *UserService
	clients *ClientService
	admins  *AdminService
	repo    ReportRepo
	logger  *slog.Logger
}

func newReportService(
	u *UserService,
	c *ClientService,
	a *AdminService,
	r ReportRepo,
	l *slog.Logger,
) *ReportService {
	return &ReportService{users: u, clients: c, admins: a, repo: r, logger: l}
}

// --- handler (top of the chain) ---

type ReportHandler struct{ svc *ReportService }

func newReportHandler(s *ReportService) *ReportHandler {
	return &ReportHandler{svc: s}
}

func TestBench_RepoServiceHandlerGraph(t *testing.T) {
	inj := injector.NewInjector()

	// Shared leaf singletons.
	db := &sql.DB{} // zero value is fine; nothing is queried in this bench
	logger := slog.Default()
	inj.Inject(db)
	inj.Inject(func() *slog.Logger { return logger })

	// Providers — registration order is intentionally scrambled to prove the
	// resolver topologically sorts on its own (no hand-ordering).
	inj.Inject(newReportHandler)
	inj.Inject(newReportService)
	inj.Inject(newAdminService)
	inj.Inject(newUserService)
	inj.Inject(newClientService)
	inj.Inject(newUserRepo)
	inj.Inject(newClientRepo)
	inj.Inject(newAdminRepo)
	inj.Inject(newReportRepo)

	// One resolve materialises the whole graph.
	h, err := injector.Get[*ReportHandler](inj)
	require.NoError(t, err)
	require.NotNil(t, h)

	// Whole graph got wired.
	require.NotNil(t, h.svc)
	require.NotNil(t, h.svc.users)
	require.NotNil(t, h.svc.clients)
	require.NotNil(t, h.svc.admins)
	require.NotNil(t, h.svc.repo)

	// Singleton sharing: the *slog.Logger seen by the report service and by a
	// leaf service is the same instance.
	assert.Same(t, h.svc.logger, h.svc.users.logger)
	assert.Same(t, logger, h.svc.clients.logger)

	// The *sql.DB leaf is shared by every repo.
	assert.Same(t, db, h.svc.users.repo.(*sqlUserRepo).db)
	assert.Same(t, db, h.svc.admins.repo.(*sqlAdminRepo).db)
}
