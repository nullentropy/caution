(ns caution.uidoc
  (:require [caution.wire :as wire]
            [clojure.data.json :as json]
            [clojure.java.io :as io]))

(defn- node->hiccup [{:strs [type name p kids]}]
  (into [(keyword type) (cond-> (or p {}) name (assoc "name" name))]
        (map node->hiccup)
        kids))

(defn parse
  "Parse a UI document string into hiccup"
  [s]
  (let [doc (json/read-str s)]
    (when-not (get doc "root")
      (throw (ex-info "caution: ui document has no root" {})))
    (node->hiccup (get doc "root"))))

(defn load-ui
  "Load a .ui.json file (a path, resource, or anything io/reader accepts)."
  [src]
  (parse (slurp (or (io/resource (str src)) src))))

(defn- wire-name [k] (wire/kebab->camel (name k)))

(defn bind
  "Merge props into named nodes: (bind doc {\"inc\" {:on-click f}
                                            \"counter\" {:text \"3\"}}).
  A binding replaces the document's version of the same prop, even though the
  two sides spell it differently (document: \"borderColor\", code:
  :border-color), and binding a prop to nil removes it"
  [form bindings]
  (if-not (vector? form)
    form
    (let [[tag attrs & kids] (if (map? (second form))
                               form
                               (into [(first form) {}] (rest form)))
          extra (get bindings (get attrs "name"))
          taken (into #{} (map wire-name) (keys extra))
          attrs (if (seq taken)
                  (into {} (remove (fn [[k _]] (taken (wire-name k)))) attrs)
                  attrs)
          kids  (map #(bind % bindings) kids)]
      (into [tag (merge attrs extra)] kids))))

(defn- node->doc
  [{:keys [type props kids]}]
  (let [nm (get props "name")
        p  (dissoc props "on" "outline" "name")]
    (cond-> {"type" type}
      nm         (assoc "name" nm)
      (seq p)    (assoc "p" p)
      (seq kids) (assoc "kids" (mapv node->doc kids)))))

(defn save-ui
  "Serialize hiccup back to a UI document string"
  [form]
  (json/write-str {"caution" 1 "root" (node->doc (wire/normalize form))}
                  :indent true))
