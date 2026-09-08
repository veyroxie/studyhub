// Command pricecheck prints what the pricing catalogue WOULD charge each
// student, beside what they were actually invoiced, for the database named by
// DATABASE_URL. It computes and never writes.
//
// It exists so the switchover can be checked against real production numbers
// without deploying anything and without touching production: the migration
// dry run restores a copy of prod, applies the migrations, and runs this
// against the copy. Same data, same code path, no risk.
//
// It calls store.CatalogPrices -- the same resolver the preview endpoint uses
// and the same one the monthly cron will call at the switchover. A second
// implementation here would defeat the entire point of checking.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"

	"studyhub/internal/core"
	"studyhub/internal/store"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is not set")
	}
	// A flag, not an env var: scripts/check.sh requires every os.Getenv the
	// backend reads to be forwarded in docker-compose.yml, and rightly so --
	// a var set in .env that compose does not pass through silently does
	// nothing. This tool never runs in the container, so it takes a flag and
	// leaves that rule intact rather than carving an exception into it.
	monthFlag := flag.String("month", "", "month to compare, YYYY-MM")
	flag.Parse()
	month := *monthFlag
	if len(month) != 7 {
		log.Fatal("pass -month=YYYY-MM")
	}

	core.InitLogger()
	db := store.InitDB(dsn)
	defer db.Close()

	// Tenant 1 is the centre. A nil claims would mean superadmin (tenant 0),
	// which owns nothing.
	claims := &core.Claims{Email: "pricecheck", Role: "admin", TenantID: 1}

	invoiced := map[string]float64{}
	rows, err := db.Query(`SELECT student_id, COALESCE(SUM(amount),0) FROM invoices
		WHERE deleted_at IS NULL AND type='Monthly' AND period=? GROUP BY student_id`, month)
	if err != nil {
		log.Fatalf("read invoices: %v", err)
	}
	func(rs *sql.Rows) {
		defer rs.Close()
		for rs.Next() {
			var id string
			var amt float64
			if rs.Scan(&id, &amt) == nil {
				invoiced[id] = amt
			}
		}
	}(rows)

	type row struct {
		name           string
		inv, cat, diff float64
		hasInv, bad    bool
		why            string
	}
	var matching, differing, unpriceable, notInvoiced int
	var out []row

	for _, sp := range store.CatalogPrices(db, claims, month+"-15") {
		amt, has := invoiced[sp.StudentID]
		if !has && len(sp.Lines) == 0 {
			continue
		}
		r := row{name: sp.StudentName, inv: amt, cat: sp.Total, hasInv: has, bad: sp.Unpriceable}
		r.diff = math.Round((sp.Total-amt)*100) / 100
		for _, l := range sp.Lines {
			if l.Problem != "" {
				if r.why != "" {
					r.why += "; "
				}
				r.why += l.ClassName + ": " + l.Problem
			}
		}
		switch {
		case sp.Unpriceable:
			unpriceable++
		case !has:
			notInvoiced++
		case r.diff == 0:
			matching++
		default:
			differing++
		}
		out = append(out, r)
	}

	// Loudest first: the rows someone has to make a decision about.
	sort.SliceStable(out, func(i, j int) bool {
		rank := func(r row) int {
			switch {
			case r.bad:
				return 0
			case r.hasInv && r.diff != 0:
				return 1
			case !r.hasInv:
				return 2
			default:
				return 3
			}
		}
		if rank(out[i]) != rank(out[j]) {
			return rank(out[i]) < rank(out[j])
		}
		return out[i].name < out[j].name
	})

	fmt.Printf("    %-26s %10s %10s   %s\n", "STUDENT", "INVOICED", "CATALOGUE", "RESULT")
	for _, r := range out {
		inv, cat, res := "-", "-", ""
		if r.hasInv {
			inv = fmt.Sprintf("%.2f", r.inv)
		}
		switch {
		case r.bad:
			res = "CANNOT PRICE - " + r.why
		case !r.hasInv:
			cat = fmt.Sprintf("%.2f", r.cat)
			res = "not invoiced"
		case r.diff == 0:
			cat = fmt.Sprintf("%.2f", r.cat)
			res = "matches"
		default:
			cat = fmt.Sprintf("%.2f", r.cat)
			res = fmt.Sprintf("DIFFERS by %+.2f", r.diff)
		}
		fmt.Printf("    %-26s %10s %10s   %s\n", trunc(r.name, 26), inv, cat, res)
	}
	fmt.Printf("\n    %s: %d match, %d differ, %d cannot be priced, %d not invoiced\n",
		month, matching, differing, unpriceable, notInvoiced)
	fmt.Println("    Nothing was written. This is what the catalogue WOULD charge.")
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "."
}
