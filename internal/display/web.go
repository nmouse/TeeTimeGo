package display

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"sort"
	"time"
)

type teeTimeJSON struct {
	Time    string  `json:"time"`
	Minutes int     `json:"minutes"`
	Players int     `json:"players"`
	Holes   int     `json:"holes"`
	Price   float64 `json:"price"`
	BookURL string  `json:"bookURL"`
}

type courseJSON struct {
	Name      string        `json:"name"`
	DistMiles float64       `json:"distMiles"`
	TeeTimes  []teeTimeJSON `json:"teeTimes"`
	Status    string        `json:"status"`
}

func buildCourseJSON(results []CourseResult) []courseJSON {
	sort.Slice(results, func(i, j int) bool {
		return results[i].DistMiles < results[j].DistMiles
	})
	courses := make([]courseJSON, 0, len(results))
	for _, r := range results {
		sort.Slice(r.TeeTimes, func(i, j int) bool {
			return r.TeeTimes[i].Time.Before(r.TeeTimes[j].Time)
		})
		cj := courseJSON{Name: r.CourseName, DistMiles: r.DistMiles}
		if len(r.TeeTimes) == 0 {
			if r.Error != "" {
				cj.Status = r.Error
			} else if r.ProviderFound {
				cj.Status = "no times available"
			} else {
				cj.Status = "no online booking found"
			}
		}
		for _, tt := range r.TeeTimes {
			cj.TeeTimes = append(cj.TeeTimes, teeTimeJSON{
				Time:    tt.Time.Format(time.Kitchen),
				Minutes: tt.Time.Hour()*60 + tt.Time.Minute(),
				Players: tt.Players,
				Holes:   tt.Holes,
				Price:   tt.Price,
				BookURL: tt.BookURL,
			})
		}
		courses = append(courses, cj)
	}
	return courses
}

// WebUIDefaults carries CLI flag values to pre-populate the web UI filter bar.
type WebUIDefaults struct {
	From    string // HH:MM or "" — pre-fills the From time input
	To      string // HH:MM or "" — pre-fills the To time input
	Players int    // pre-selects the Min spots dropdown
	Holes   int    // pre-selects the Holes dropdown (9, 18, or 0 for any)
}

// ServeWeb starts a local HTTP server and opens the browser to display results.
// defaults pre-populates the filter bar with the CLI flag values that were used.
// fetchFn is called when the user changes the date in the UI; it receives the new
// date and should return deduplicated, filtered results for that date.
// Blocks until the process is interrupted.
func ServeWeb(results []CourseResult, location string, date time.Time, defaults WebUIDefaults, fetchFn func(time.Time) ([]CourseResult, error)) error {
	tmpl, err := template.New("").Parse(webTemplate)
	if err != nil {
		return fmt.Errorf("parsing template: %w", err)
	}

	type pageData struct {
		Data     template.JS
		Location string
		Date     string
		DateISO  string
		From     string
		To       string
		Players  int
		Holes    int
	}

	buildPage := func(r []CourseResult, d time.Time) (pageData, error) {
		b, err := json.Marshal(buildCourseJSON(r))
		if err != nil {
			return pageData{}, fmt.Errorf("serializing results: %w", err)
		}
		return pageData{
			Data:     template.JS(b),
			Location: location,
			Date:     d.Format("January 2, 2006"),
			DateISO:  d.Format("2006-01-02"),
			From:     defaults.From,
			To:       defaults.To,
			Players:  defaults.Players,
			Holes:    defaults.Holes,
		}, nil
	}

	initial, err := buildPage(results, date)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, initial)
	})
	mux.HandleFunc("/teetimes", func(w http.ResponseWriter, r *http.Request) {
		dateStr := r.URL.Query().Get("date")
		d, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			http.Error(w, "invalid date: must be YYYY-MM-DD", http.StatusBadRequest)
			return
		}
		newResults, err := fetchFn(d)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(buildCourseJSON(newResults))
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("starting server: %w", err)
	}
	addr := fmt.Sprintf("http://localhost:%d", ln.Addr().(*net.TCPAddr).Port)
	fmt.Printf("Serving results at %s  (Ctrl+C to exit)\n", addr)
	go openBrowser(addr)

	return http.Serve(ln, mux)
}

func openBrowser(url string) {
	var cmd string
	args := []string{url}
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd = "cmd"
		args = append([]string{"/c", "start"}, args...)
	default:
		cmd = "xdg-open"
	}
	exec.Command(cmd, args...).Start()
}

const webTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Tee Times — {{.Location}}</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#f4f6f4;color:#1a1a1a;font-size:14px}
header{background:#1a5c2a;color:#fff;padding:16px 24px}
header h1{font-size:1.1rem;font-weight:600;letter-spacing:.01em}
header p{font-size:.82rem;opacity:.75;margin-top:3px}
.bar{background:#fff;border-bottom:1px solid #e2e8e2;padding:10px 24px;display:flex;gap:20px;align-items:center;flex-wrap:wrap}
.bar label{display:flex;align-items:center;gap:6px;font-size:.82rem;color:#444;white-space:nowrap}
.bar input[type=time],.bar input[type=date],.bar select{border:1px solid #ccc;border-radius:4px;padding:4px 7px;font-size:.82rem;background:#fff;color:#1a1a1a}
.bar input[type=checkbox]{accent-color:#1a5c2a}
.wrap{padding:20px 24px}
.summary{font-size:.82rem;color:#666;margin-bottom:10px}
table{width:100%;border-collapse:collapse;background:#fff;border-radius:8px;overflow:hidden;box-shadow:0 1px 3px rgba(0,0,0,.1)}
thead th{background:#f0f4f0;padding:9px 14px;text-align:left;font-size:.75rem;font-weight:600;text-transform:uppercase;letter-spacing:.06em;color:#555;white-space:nowrap;cursor:pointer;user-select:none}
thead th:hover{background:#e4ece4}
thead th.asc::after{content:" ↑"}
thead th.desc::after{content:" ↓"}
td{padding:9px 14px;border-top:1px solid #f0f0f0;vertical-align:middle}
tr.course-row td{background:#fafbfa;border-top:2px solid #e2e8e2}
tr.course-row.has-times{cursor:pointer}
tr.course-row.has-times:hover td{background:#edf2ed}
tr.course-row td:first-child{font-weight:600;color:#1a5c2a}
tr.teetime-row td{border-top:1px solid #f0f0f0}
tr.no-time td{color:#999;font-style:italic}
tr.loading td{text-align:center;padding:24px;color:#888;font-style:italic}
tr.error td{text-align:center;padding:24px;color:#c00}
.arrow{display:inline-block;width:1.1em;font-size:.7rem;color:#888;transition:transform .1s}
.dist{color:#888;font-size:.8rem}
.count{color:#555;font-size:.82rem}
.range{color:#555}
.price{font-weight:500}
a.book{display:inline-block;padding:3px 10px;background:#1a5c2a;color:#fff;border-radius:4px;text-decoration:none;font-size:.78rem;white-space:nowrap}
a.book:hover{background:#236b35}
</style>
</head>
<body>
<header>
  <h1>Tee Times near {{.Location}}</h1>
  <p id="header-date">{{.Date}}</p>
</header>
<div class="bar">
  <label>Date <input type="date" id="f-date" value="{{.DateISO}}"></label>
  <label>From <input type="time" id="f-from" value="{{.From}}"></label>
  <label>To <input type="time" id="f-to" value="{{.To}}"></label>
  <label>Min spots
    <select id="f-spots">
      <option value="1"{{if eq .Players 1}} selected{{end}}>Any</option>
      <option value="2"{{if eq .Players 2}} selected{{end}}>2+</option>
      <option value="3"{{if eq .Players 3}} selected{{end}}>3+</option>
      <option value="4"{{if eq .Players 4}} selected{{end}}>4</option>
    </select>
  </label>
  <label>Holes
    <select id="f-holes">
      <option value=""{{if not .Holes}} selected{{end}}>Any</option>
      <option value="9"{{if eq .Holes 9}} selected{{end}}>9</option>
      <option value="18"{{if eq .Holes 18}} selected{{end}}>18</option>
    </select>
  </label>
  <label>Sort
    <select id="f-sort">
      <option value="dist">Distance</option>
      <option value="time">Earliest time</option>
      <option value="price">Lowest price</option>
    </select>
  </label>
  <label><input type="checkbox" id="f-hide"> Hide unavailable</label>
</div>
<div class="wrap">
  <p class="summary" id="summary"></p>
  <table>
    <thead>
      <tr>
        <th data-col="name">Course</th>
        <th data-col="dist">Dist</th>
        <th data-col="time">Time</th>
        <th data-col="spots">Avail.</th>
        <th data-col="holes">Holes</th>
        <th data-col="price">Price</th>
        <th></th>
      </tr>
    </thead>
    <tbody id="tbody"></tbody>
  </table>
</div>
<script>
let raw = {{.Data}};
const expanded = new Set();

function toggleCourse(name) {
  if (expanded.has(name)) expanded.delete(name); else expanded.add(name);
  render();
}

function toMins(val){ if(!val)return -1; const[h,m]=val.split(':').map(Number); return h*60+m; }
function esc(s){ return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;'); }
function fmt(p){ return p>0?'$'+p.toFixed(2):'--'; }

function render(){
  const fromM  = toMins(document.getElementById('f-from').value);
  const toM    = toMins(document.getElementById('f-to').value);
  const spots  = parseInt(document.getElementById('f-spots').value)||1;
  const holes  = document.getElementById('f-holes').value;
  const sortBy = document.getElementById('f-sort').value;
  const hide   = document.getElementById('f-hide').checked;

  const courses = raw.map(c=>({
    ...c,
    vis: (c.teeTimes||[]).filter(t=>
      (fromM<0||t.minutes>=fromM)&&
      (toM<0||t.minutes<=toM)&&
      t.players>=spots&&
      (!holes||t.holes===parseInt(holes))
    )
  }));

  courses.sort((a,b)=>{
    if(sortBy==='dist') return a.distMiles-b.distMiles;
    if(sortBy==='time'){
      const am=a.vis.length?a.vis[0].minutes:1e9, bm=b.vis.length?b.vis[0].minutes:1e9;
      return am!==bm?am-bm:a.distMiles-b.distMiles;
    }
    if(sortBy==='price'){
      const ap=a.vis.length?Math.min(...a.vis.map(t=>t.price)):1e9, bp=b.vis.length?Math.min(...b.vis.map(t=>t.price)):1e9;
      return ap!==bp?ap-bp:a.distMiles-b.distMiles;
    }
  });

  let totalTimes=0, coursesWithTimes=0;
  const rows=[];

  courses.forEach(c=>{
    if(c.vis.length){
      coursesWithTimes++; totalTimes+=c.vis.length;
      const open = expanded.has(c.name);
      const arrow = open ? '▼' : '▶';
      const first = c.vis[0].time;
      const last  = c.vis[c.vis.length-1].time;
      const range = c.vis.length>1 ? first+' – '+last : first;
      const minP  = Math.min(...c.vis.map(t=>t.price));
      const cnt   = c.vis.length+' time'+(c.vis.length!==1?'s':'');

      rows.push(
        '<tr class="course-row has-times" data-name="'+esc(c.name)+'" onclick="toggleCourse(this.dataset.name)">' +
        '<td><span class="arrow">'+arrow+'</span> '+esc(c.name)+'</td>'+
        '<td class="dist">'+c.distMiles.toFixed(1)+'mi</td>'+
        '<td class="range">'+esc(range)+'</td>'+
        '<td class="count">'+cnt+'</td>'+
        '<td></td>'+
        '<td class="price">'+(minP>0?'from '+fmt(minP):'--')+'</td>'+
        '<td></td>'+
        '</tr>'
      );

      if(open){
        c.vis.forEach(t=>{
          rows.push(
            '<tr class="teetime-row">'+
            '<td></td>'+
            '<td></td>'+
            '<td>'+esc(t.time)+'</td>'+
            '<td>'+t.players+'</td>'+
            '<td>'+t.holes+'</td>'+
            '<td class="price">'+fmt(t.price)+'</td>'+
            '<td>'+(t.bookURL?'<a class="book" href="'+esc(t.bookURL)+'" target="_blank">Book →</a>':'')+'</td>'+
            '</tr>'
          );
        });
      }
    } else if(!hide){
      rows.push('<tr class="no-time course-row">'+
        '<td>'+esc(c.name)+'</td>'+
        '<td class="dist">'+c.distMiles.toFixed(1)+'mi</td>'+
        '<td colspan="5">'+esc(c.status||'')+'</td>'+
        '</tr>');
    }
  });

  document.getElementById('tbody').innerHTML=rows.join('');
  document.getElementById('summary').textContent=
    coursesWithTimes+' course'+(coursesWithTimes!==1?'s':'')+' with times · '+totalTimes+' tee time'+(totalTimes!==1?'s':'');
}

document.getElementById('f-date').addEventListener('change', async function() {
  const d = this.value;
  if (!d) return;
  document.getElementById('tbody').innerHTML =
    '<tr class="loading"><td colspan="7">Fetching tee times… (may take ~10 seconds)</td></tr>';
  document.getElementById('summary').textContent = 'Loading…';
  try {
    const resp = await fetch('/teetimes?date=' + encodeURIComponent(d));
    if (!resp.ok) throw new Error(await resp.text());
    raw = await resp.json();
    // update header date display
    const dt = new Date(d + 'T12:00:00');
    document.getElementById('header-date').textContent =
      dt.toLocaleDateString('en-US', {year:'numeric', month:'long', day:'numeric'});
    render();
  } catch(e) {
    document.getElementById('tbody').innerHTML =
      '<tr class="error"><td colspan="7">'+esc(e)+'</td></tr>';
    document.getElementById('summary').textContent = 'Error loading results';
  }
});

['f-from','f-to','f-spots','f-holes','f-sort','f-hide'].forEach(id=>
  document.getElementById(id).addEventListener('change',render));
document.getElementById('f-from').addEventListener('input',render);
document.getElementById('f-to').addEventListener('input',render);
render();
</script>
</body>
</html>`
