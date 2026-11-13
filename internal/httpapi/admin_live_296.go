package httpapi

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// Bundle 296 (H4): the web / in-app admin map shows only live links.
//
//   - Phones send {"type":"link_report_296","neighbors":[...]} with only the
//     links proven in the last 10 s, every 15 s and at once on a change.
//     An empty list = "no links now" (the old presence frame without the
//     field keeps the last report, as before).
//   - A report counts 45 s (neighborTTL216) and only while its phone is on
//     the server (linkMap216).
//   - While an admin watches, the map is checked every 5 s and pushed at once
//     when it changed (a report running out used to push nothing for 30 s).

const (
	linkReportType296  = "link_report_296"
	liveSweep296       = 5 * time.Second
	liveSweepForced296 = 6 // every 6th check (30 s) a frame anyway
)

func (a *API) allowLinkReport296(deviceID string) bool {
	return a.allowDeviceAction("link-report", deviceID, 30, 120)
}

// noteLinkReport296: nil (field missing) is ignored; [] clears the links.
func (a *API) noteLinkReport296(deviceID string, list []Neighbor) {
	if list == nil {
		return
	}
	a.noteNeighbors216(deviceID, list)
}

// linkMapSig296 is the part of the map a viewer sees (who is online, how a
// phone is reached, which lines). Same signature = nothing to push.
func linkMapSig296(nodes []linkNode216, edges []linkEdge216) string {
	parts := make([]string, 0, len(nodes)+len(edges))
	for _, n := range nodes {
		parts = append(parts, "n|"+n.DeviceID+"|"+strconv.FormatBool(n.Online)+"|"+n.Link+"|"+strconv.FormatBool(n.Blocked))
	}
	for _, e := range edges {
		x, y := e.From, e.To
		if x > y {
			x, y = y, x
		}
		parts = append(parts, "e|"+x+"|"+y+"|"+e.Link)
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}

// sweepTick296: one 5 s check. Returns the new signature and whether a frame
// must go out.
func (a *API) sweepTick296(last string, tick int) (string, bool) {
	nodes, edges, err := a.linkMap216(time.Now())
	if err != nil {
		return last, tick%liveSweepForced296 == 0
	}
	sig := linkMapSig296(nodes, edges)
	return sig, sig != last || tick%liveSweepForced296 == 0
}
