(ns caution.core
  (:require [caution.server :as server]
            [caution.session :as session]
            [caution.uidoc :as uidoc]
            [caution.wire :as wire]
            [clojure.java.io :as io]))

(def serve
  "Run a caution app. Returns a zero-arg fn that stops the server.

  Options:
    :port    listen port (default 8787)
    :init    initial per-session state - a value or a zero-arg fn
    :view    (fn [state session] hiccup) - required
    :app-js  path to the prebuilt terminal bundle
             (default \"../dist/app.js\"; build with
              `go run -C server ./cmd/bundle -o ../dist/app.js`)
    :authorize (fn [req] identity) - runs on every connection including
             resumes; throw to reject with 403. A resume only re-adopts a
             session whose identity matches.
    :allowed-origins  vector of allowed Origins (default: same-origin only,
             \"*\" for dev)
    :on-mount (fn [session]) - after a session is created (start tickers here)
    :on-close (fn [session]) - after it expires for good
    :routes  {\"/path\" ring-handler}
    :static  {\"/prefix/\" \"directory\"}"
  server/serve)

(def state        "Current app state." session/state)
(def swap-state!  "Apply f to session state; re-renders and patches the client."
  session/swap-state!)
(def set-theme!   "Push design tokens: a map of token name -> color string."
  session/set-theme!)
(def set-metrics! "Push metric tokens: a map of token name -> logical-px number
  (\"radius.control\", \"border.width\", ...) - the geometry half of theming."
  session/set-metrics!)

(def compact-metrics
  "The dense look"
  {"radius.control" 4 "radius.popover" 6 "radius.panel" 8
   "control.height" 26 "control.padX" 10 "row.height" 22
   "table.headerHeight" 26 "table.cellPadX" 8 "checkbox.size" 15
   "slider.thumb" 7 "slider.track" 3 "dialog.titleHeight" 38
   "space.pad" 7 "space.gap" 7})

(def comfortable-metrics
  "The airy look"
  {"radius.control" 8 "radius.popover" 10 "radius.panel" 14
   "control.height" 38 "control.padX" 20 "row.height" 34
   "table.headerHeight" 38 "table.cellPadX" 16 "checkbox.size" 20
   "slider.thumb" 10 "slider.track" 5 "dialog.titleHeight" 56
   "space.pad" 13 "space.gap" 13})
(def set-title!   "Name the window (browser tab title / native titlebar)."
  session/set-title!)
(def set-keys!    "Register session-wide shortcuts: [{:key \"cmd+j\" :handler f} ...]."
  session/set-keys!)
(def set-aux!     "Handle mouse buttons 4 (back) and 5 (forward): (fn [state button])."
  session/set-aux!)
(def set-fullscreen! "Handle entering/leaving system fullscreen: (fn [state on?])."
  session/set-fullscreen!)
(def preload!     "Warm client resource caches (images) ahead of first use."
  session/preload!)
(def preload-sounds! "Decode WAV sources on the client ahead of their first play!."
  session/preload-sounds!)
(def set-sounds!  "Set the gesture sound table: {\"press\" \"/click.wav\" ...}."
  session/set-sounds!)
(def play!        "Play a WAV once on the client (URL or data: URI)."
  session/play!)
(def loop!        "Play a WAV on repeat until stop!." session/loop!)
(def stop!        "Stop every playing instance of a source." session/stop!)
(def stop-all!    "Stop every sound." session/stop-all!)
(def set-menu!    "Replace the menu bar: a vector of {:title _ :items [...]},
  where an item is {:title _ :key \"n\" :on-pick (fn [state])}, {:sep true},
  or a submenu {:title _ :items [...]}. Realized natively (NSMenu) by the
  native terminal; picks run like any other handler. Rides mounts - set it
  in :on-mount and it arrives with the first paint."
  session/set-menu!)
(def identity-of  "Whatever :authorize returned for this session." session/identity-of)
(def request      "The HTTP request that opened the session." session/request)
(def viewport     "Client window size as [w h] logical px." session/viewport)
(def alive?       "False once the session has expired - stop background loops."
  session/alive?)
(def closed?      "True while no client is attached." session/closed?)
(def rerender-all!
  "Hot reload: re-render every live session in place (state survives). Views
  passed as vars (#'view) re-render on re-def automatically; call this after
  redefining helpers."
  session/rerender-all!)

(defn- bump-cmd [state k prop]
  (update-in state [::cmd k prop] (fnil inc 0)))

(defn focus
  "Ask the client to focus (and reveal) the node that splices
  `(one-shots state k)`. Pure - compose into any handler:
      (fn [s] (-> s (assoc :panel :compose) (focus :composer)))"
  [state k]
  (bump-cmd state k :focus-seq))

(defn reveal
  "Scroll the node into view without taking focus."
  [state k]
  (bump-cmd state k :reveal-seq))

(defn clear-value
  "Force-clear a text field even while the user is editing it.
      (-> state (assoc :note \"\") (clear-value :composer))"
  [state k]
  (bump-cmd state k :reset-seq))

(defn override-value
  "Make the client accept the value this same render carries even while the
  user is editing
      (-> state (assoc :prefix v') (override-value :prefix))"
  [state k]
  (bump-cmd state k :override-seq))

(defn one-shots
  "The pending command props for k. Merge into the target node's props:
      [:textfield (merge {:value (:prefix s) :on-commit f}
                         (one-shots s :prefix))]"
  [state k]
  (get-in state [::cmd k] {}))

;; -- trees -----------------------------------------------------------------------
;;
;; A :tree node is the table in tree mode: same columns, same virtualized
;; rows, plus per-row hierarchy meta. The app owns the items and the expanded
;; set and these pure helpers flatten the visible slice into the shape the :rows fn must return.
;;
;;   [:tree {:columns [{:key "n" :title "Name" :weight 1}]
;;           :row-count (c/tree-row-count items (:open s))
;;           :rows (fn [s start end] (c/tree-rows items (:open s) start end))
;;           :on-toggle (fn [s v] (update s :open c/toggle-member (get v "key")))
;;           :on-row-select (fn [s v] (assoc s :picked (get v "key")))}]

(defn- tree-flatten [items expanded depth]
  (into []
        (mapcat (fn [{:keys [key cells kids]}]
                  (let [open? (and (contains? expanded key) (seq kids))]
                    (cons {:cells cells
                           :meta (cond-> {:key key :d depth}
                                   (seq kids) (assoc :k true)
                                   open? (assoc :x true))}
                          (when open? (tree-flatten kids expanded (inc depth)))))))
        items))

(defn tree-rows
  "The [start, end] window of a tree's visible rows, for a :tree node's
  :rows fn. items are {:key _ :cells [..] :kids [..]}; expanded is a set of
  open keys."
  [items expanded start end]
  (let [flat (tree-flatten items expanded 0)]
    (subvec flat (min start (count flat)) (min (inc end) (count flat)))))

(defn tree-row-count
  "How many rows the tree shows with this expanded set - the :row-count prop."
  [items expanded]
  (count (tree-flatten items expanded 0)))

(defn toggle-member
  "Set helper for :on-toggle handlers: flip k's membership."
  [s k]
  (let [s (or s #{})]
    (if (contains? s k) (disj s k) (conj s k))))

;; -- UI documents (nibs) --------------------------------------------------------

(def load-ui "Load a .ui.json document as hiccup." uidoc/load-ui)
(def parse-ui "Parse a .ui.json string as hiccup." uidoc/parse)
(def bind    "Merge props/handlers into named nodes; returns a new tree." uidoc/bind)
(def save-ui "Serialize hiccup back to a .ui.json string." uidoc/save-ui)

(defonce ^:private watched-docs (atom {}))

(defn- watch-file!
  "Poll the file's mtime and push reparses into !doc. Polling (4x/s) keeps it
  dependency-free and identical on every platform. A torn read mid-save just
  logs and waits for the next write."
  [^java.io.File file !doc path]
  (doto (Thread.
         (fn []
           (loop [seen (.lastModified file)]
             (Thread/sleep 250)
             (let [m (.lastModified file)]
               (when (not= m seen)
                 (try
                   (let [doc (uidoc/parse (slurp file))]
                     (when (not= doc @!doc)
                       (reset! !doc doc)
                       (session/rerender-all!)))
                   (catch Exception e
                     (println "caution: watch-ui" path "-" (.getMessage e)))))
               (recur m)))))
    (.setName (str "caution-watch " path))
    (.setDaemon true)
    (.start)))

(defn watch-ui
  "load-ui, but live. Deref it in the view:

      (defonce doc (watch-ui \"resources/window.ui.json\"))
      (defn view [state _] (bind @doc {\"inc\" {:on-click ...}}))

  Files only, not classpath resources."
  [path]
  (let [file (io/file path)
        k    (.getCanonicalPath file)]
    (or (get @watched-docs k)
        (locking watched-docs
          (or (get @watched-docs k)
              (let [!doc (atom (uidoc/parse (slurp file)))]
                (watch-file! file !doc path)
                (swap! watched-docs assoc k !doc)
                !doc))))))

;; -- for tests and tooling --------------------------------------------------------

(def normalize "Hiccup -> internal node (types, props, handlers, kids)." wire/normalize)
(def ->wire    "Internal node -> the JSON shape the client inflates." wire/->wire)
