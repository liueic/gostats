package server

import (
	"bytes"
	"html/template"
	"net/http"
	"strings"
)

const openSourceRepoURL = "https://github.com/liueic/gostats"

type indexPageData struct {
	RepoURL   string
	Endpoints []string
}

var indexPageTemplate = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>gostats 控制面板</title>
  <style>
    :root {
      --ink: #0f172a;
      --muted: #475569;
      --line: #dbe3ef;
      --paper: #f6fbff;
      --brand: #0f766e;
      --brand-2: #065f46;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      color: var(--ink);
      font-family: "Noto Sans SC", "PingFang SC", "Microsoft YaHei", sans-serif;
      background: radial-gradient(1200px 600px at 10% -10%, #d7f9f3 0%, #f7fbff 55%, #ffffff 100%);
    }
    .wrap { max-width: 980px; margin: 0 auto; padding: 24px 16px 48px; }
    .nav {
      display: flex; align-items: center; justify-content: space-between; gap: 12px;
      padding: 14px 16px; border: 1px solid var(--line); background: rgba(255,255,255,.85);
      border-radius: 14px; backdrop-filter: blur(8px);
    }
    .brand { font-size: 20px; font-weight: 700; letter-spacing: .2px; }
    .link {
      color: var(--brand-2); text-decoration: none; font-weight: 600;
      border-bottom: 1px solid transparent;
    }
    .link:hover { border-bottom-color: var(--brand-2); }
    .hero { margin-top: 18px; color: var(--muted); line-height: 1.6; }
    .hero h1 {
      margin: 0 0 8px; font-size: 28px; line-height: 1.25; color: var(--ink);
    }
    .hero p { margin: 0; }
    .cards {
      margin-top: 14px;
      display: grid; gap: 10px;
      grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    }
    .card {
      background: rgba(255,255,255,.88);
      border: 1px solid var(--line);
      border-radius: 12px;
      padding: 12px 13px;
      line-height: 1.55;
      color: var(--muted);
      font-size: 14px;
    }
    .card b { color: var(--ink); }
    .howto {
      margin-top: 14px;
      background: #fff;
      border: 1px dashed #b8c6dc;
      border-radius: 12px;
      padding: 12px 14px;
      color: #334155;
      font-size: 14px;
      line-height: 1.65;
    }
    .howto code {
      background: #f1f5f9;
      padding: 2px 6px;
      border-radius: 6px;
      border: 1px solid #e2e8f0;
    }
    .panel {
      margin-top: 18px; background: #fff; border: 1px solid var(--line);
      border-radius: 16px; padding: 18px;
      box-shadow: 0 12px 30px rgba(15, 23, 42, .06);
    }
    .grid {
      display: grid; gap: 12px;
      grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    }
    label { display: block; font-size: 13px; color: var(--muted); margin-bottom: 6px; }
    input, textarea {
      width: 100%; border: 1px solid #cbd5e1; border-radius: 10px;
      padding: 10px 12px; font: inherit; color: var(--ink); background: #fff;
    }
    textarea { min-height: 78px; resize: vertical; }
    .actions { display: flex; flex-wrap: wrap; gap: 10px; margin-top: 12px; }
    button {
      appearance: none; border: none; border-radius: 10px; cursor: pointer;
      background: linear-gradient(120deg, var(--brand), var(--brand-2));
      color: #fff; padding: 10px 14px; font: inherit; font-weight: 600;
    }
    button.secondary { background: #0f172a; }
    .hint { margin-top: 10px; color: var(--muted); font-size: 13px; }
    .links { margin-top: 12px; display: grid; gap: 8px; }
    .links a {
      color: #0b4f47; text-decoration: none; background: var(--paper);
      border: 1px solid #d6efe9; border-radius: 10px; padding: 8px 10px;
      display: block; word-break: break-all;
    }
    .api {
      margin-top: 18px; background: #fff; border: 1px solid var(--line);
      border-radius: 16px; padding: 18px;
    }
    ul { margin: 0; padding-left: 18px; line-height: 1.7; }
  </style>
</head>
<body>
  <div class="wrap">
    <nav class="nav">
      <div class="brand">gostats 控制面板</div>
      <a class="link" href="{{.RepoURL}}" target="_blank" rel="noopener noreferrer">开源地址</a>
    </nav>

    <section class="hero">
      <h1>一个给静态博客用的实时数据后端</h1>
      <p>
        gostats 用来给 Astro/Hexo 这类无后端站点提供统一 JSON 接口。
        你可以在这里快速生成 GitHub / Steam / Spotify / Bangumi 的 API URL，直接复制到前端页面中使用。
      </p>
      <div class="cards">
        <div class="card"><b>它解决什么问题？</b><br>静态博客无法直接安全访问第三方 API，这个服务负责统一拉取、清洗、缓存和输出。</div>
        <div class="card"><b>支持哪些数据？</b><br>GitHub Followers、Steam 游戏总数/总时长/近 2 周时长、Spotify 当前播放与收藏曲目数、Bangumi 动画/游戏总收藏 + 在看/看过/想看（游戏对应在玩/玩过/想玩）。</div>
        <div class="card"><b>首次 Spotify 授权</b><br>打开 <code>/spotify/auth/start</code> 完成授权后，服务会自动保存并刷新 refresh token。</div>
      </div>
      <div class="howto">
        <b>快速上手：</b> 1) 填写下方参数 2) 点击“组装 URL” 3) 复制批量接口到你的前端。
        推荐使用 <code>/stats.json</code> 一次性拉取并展示。
      </div>
    </section>

    <section class="panel">
      <div class="grid">
        <div>
          <label for="baseURL">服务地址</label>
          <input id="baseURL" readonly>
        </div>
        <div>
          <label for="githubName">GitHub 用户名（可选）</label>
          <input id="githubName" placeholder="例如: octocat">
        </div>
        <div>
          <label for="steamID">Steam ID / Vanity（可选）</label>
          <input id="steamID" placeholder="例如: 76561198000000000 或 vanity">
        </div>
        <div>
          <label for="spotifyKey">Spotify key（可选）</label>
          <input id="spotifyKey" value="me">
        </div>
        <div>
          <label for="bangumiName">Bangumi 用户名（可选）</label>
          <input id="bangumiName" placeholder="例如: your_username">
        </div>
      </div>

      <div class="actions">
        <button id="buildBtn">组装 URL</button>
        <button id="copyBtn" class="secondary" type="button">复制 URL</button>
        <button id="openBtn" class="secondary" type="button">新窗口打开</button>
      </div>

      <div class="hint">批量接口（推荐）</div>
      <textarea id="batchURL" readonly></textarea>
      <div id="singleLinks" class="links"></div>
    </section>

    <section class="api">
      <div style="font-weight:700; margin-bottom:8px;">可用接口</div>
      <ul>
        {{range .Endpoints}}
        <li><code>{{.}}</code></li>
        {{end}}
      </ul>
    </section>
  </div>

  <script>
    const base = window.location.origin;
    const baseEl = document.getElementById("baseURL");
    const githubEl = document.getElementById("githubName");
    const steamEl = document.getElementById("steamID");
    const spotifyEl = document.getElementById("spotifyKey");
    const bangumiEl = document.getElementById("bangumiName");
    const batchEl = document.getElementById("batchURL");
    const linksEl = document.getElementById("singleLinks");
    const buildBtn = document.getElementById("buildBtn");
    const copyBtn = document.getElementById("copyBtn");
    const openBtn = document.getElementById("openBtn");

    baseEl.value = base;

    function buildURLs() {
      const github = githubEl.value.trim();
      const steam = steamEl.value.trim();
      const spotify = spotifyEl.value.trim();
      const bangumi = bangumiEl.value.trim();

      const params = new URLSearchParams();
      if (github) params.set("github", github);
      if (steam) params.set("steam", steam);
      if (spotify) params.set("spotify", spotify);
      if (bangumi) params.set("bangumi", bangumi);

      const query = params.toString();
      batchEl.value = query ? (base + "/stats.json?" + query) : (base + "/stats.json");

      const items = [];
      if (github) {
        items.push(["GitHub Followers", base + "/stats/github/" + encodeURIComponent(github)]);
      }
      if (steam) {
        items.push(["Steam Games", base + "/stats/steamgames/" + encodeURIComponent(steam)]);
        items.push(["Steam Playtime", base + "/stats/steamtime/" + encodeURIComponent(steam)]);
        items.push(["Steam Playtime (2 Weeks)", base + "/stats/steam2weekstime/" + encodeURIComponent(steam)]);
      }
      if (spotify) {
        items.push(["Spotify Playing", base + "/stats/spotifyplaying/" + encodeURIComponent(spotify)]);
        items.push(["Spotify Saved Tracks", base + "/stats/spotifysaved/" + encodeURIComponent(spotify)]);
      }
      if (bangumi) {
        items.push(["Bangumi Anime Collections", base + "/stats/bangumianime/" + encodeURIComponent(bangumi)]);
        items.push(["Bangumi Game Collections", base + "/stats/bangumigame/" + encodeURIComponent(bangumi)]);
        items.push(["Bangumi Anime Watching", base + "/stats/bangumianimewatching/" + encodeURIComponent(bangumi)]);
        items.push(["Bangumi Anime Watched", base + "/stats/bangumianimewatched/" + encodeURIComponent(bangumi)]);
        items.push(["Bangumi Anime Wish", base + "/stats/bangumianimewish/" + encodeURIComponent(bangumi)]);
        items.push(["Bangumi Game Playing", base + "/stats/bangumigameplaying/" + encodeURIComponent(bangumi)]);
        items.push(["Bangumi Game Played", base + "/stats/bangumigameplayed/" + encodeURIComponent(bangumi)]);
        items.push(["Bangumi Game Wish", base + "/stats/bangumigamewish/" + encodeURIComponent(bangumi)]);
      }

      linksEl.innerHTML = "";
      if (items.length === 0) {
        const p = document.createElement("div");
        p.className = "hint";
        p.textContent = "请先填写至少一个参数（GitHub / Steam / Spotify / Bangumi）。";
        linksEl.appendChild(p);
        return;
      }

      for (const [name, url] of items) {
        const a = document.createElement("a");
        a.href = url;
        a.target = "_blank";
        a.rel = "noopener noreferrer";
        a.textContent = name + ": " + url;
        linksEl.appendChild(a);
      }
    }

    buildBtn.addEventListener("click", buildURLs);
    copyBtn.addEventListener("click", async () => {
      if (!batchEl.value) buildURLs();
      try {
        await navigator.clipboard.writeText(batchEl.value);
        copyBtn.textContent = "已复制";
        setTimeout(() => { copyBtn.textContent = "复制 URL"; }, 1200);
      } catch (_) {
        copyBtn.textContent = "复制失败";
        setTimeout(() => { copyBtn.textContent = "复制 URL"; }, 1200);
      }
    });
    openBtn.addEventListener("click", () => {
      if (!batchEl.value) buildURLs();
      if (batchEl.value) window.open(batchEl.value, "_blank", "noopener,noreferrer");
    });

    buildURLs();
  </script>
</body>
</html>
`))

func writeIndexPage(w http.ResponseWriter) {
	var buf bytes.Buffer
	if err := indexPageTemplate.Execute(&buf, indexPageData{
		RepoURL:   strings.TrimSpace(openSourceRepoURL),
		Endpoints: indexEndpoints(),
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "render index page failed"})
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}
