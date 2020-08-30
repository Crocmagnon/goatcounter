// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

package cron

import (
	"context"

	"zgo.at/errors"
	"zgo.at/gadget"
	"zgo.at/goatcounter"
	"zgo.at/zdb"
	"zgo.at/zdb/bulk"
)

func updateBrowserStats(ctx context.Context, hits []goatcounter.Hit, isReindex bool) error {
	return zdb.TX(ctx, func(ctx context.Context, tx zdb.DB) error {
		// Group by day + browser + version.
		type gt struct {
			count       int
			countUnique int
			day         string
			browser     string
			version     string
		}
		grouped := map[string]gt{}
		for _, h := range hits {
			if h.Bot > 0 {
				continue
			}

			browser, version := getBrowser(h.Browser)
			if browser == "" {
				continue
			}

			day := h.CreatedAt.Format("2006-01-02")
			k := day + browser + version
			v := grouped[k]
			if v.count == 0 {
				v.day = day
				v.browser = browser
				v.version = version
				if !isReindex {
					var err error
					v.count, v.countUnique, err = existingBrowserStats(ctx, tx,
						h.Site, day, v.browser, v.version)
					if err != nil {
						return err
					}
				}
			}

			v.count += 1
			if h.FirstVisit {
				v.countUnique += 1
			}
			grouped[k] = v
		}

		siteID := goatcounter.MustGetSite(ctx).ID
		ins := bulk.NewInsert(ctx, "browser_stats", []string{"site", "day",
			"browser", "version", "count", "count_unique"})
		for _, v := range grouped {
			ins.Values(siteID, v.day, v.browser, v.version, v.count, v.countUnique)
		}
		return ins.Finish()
	})
}

func existingBrowserStats(
	txctx context.Context, tx zdb.DB, siteID int64,
	day, browser, version string,
) (int, int, error) {

	var c []struct {
		Count       int `db:"count"`
		CountUnique int `db:"count_unique"`
	}
	err := tx.SelectContext(txctx, &c, `/* existingBrowserStats */
		select count, count_unique from browser_stats
		where site=$1 and day=$2 and browser=$3 and version=$4 limit 1`,
		siteID, day, browser, version)
	if err != nil {
		return 0, 0, errors.Wrap(err, "select")
	}
	if len(c) == 0 {
		return 0, 0, nil
	}

	_, err = tx.ExecContext(txctx, `delete from browser_stats where
		site=$1 and day=$2 and browser=$3 and version=$4`,
		siteID, day, browser, version)
	return c[0].Count, c[0].CountUnique, errors.Wrap(err, "delete")
}

func getBrowser(uaHeader string) (string, string) {
	ua := gadget.Parse(uaHeader)
	return ua.BrowserName, ua.BrowserVersion
}
