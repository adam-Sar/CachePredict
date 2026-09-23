"""
Synthetic e-commerce API session data generator for Next API Call Prediction
(matching the Go API in main.go + the example flows requested).

Outputs synthetic_api_calls.csv with one row per (session, step), and explicit
current_call -> next_call columns ready for LSTM training.
"""

import csv
import json
import random
from pathlib import Path

random.seed(42)

# ---------------------------------------------------------------------------
# Catalog
# ---------------------------------------------------------------------------
# (id, category, gender, price_bucket)
WOMEN = [
    (42, "skirt",  "female", "mid"),   # user's literal example id
    (44, "skirt",  "female", "mid"),   # user's literal example id (female skirt)
    (47, "skirt",  "female", "mid"),
    (55, "dress",  "female", "high"),
    (61, "blouse", "female", "low"),
    (73, "heels",  "female", "high"),
    (88, "handbag","female", "high"),
]
MEN = [
    (11, "shirt",   "male", "low"),
    (15, "pants",   "male", "mid"),
    (22, "sneakers","male", "mid"),
    (33, "jacket",  "male", "high"),
    (39, "watch",   "male", "high"),
]
UNISEX = [
    (101, "sunglasses", "unisex", "low"),
    (105, "belt",       "unisex", "low"),
    (110, "scarf",      "unisex", "low"),
    (120, "wallet",     "unisex", "mid"),
]
ALL_PRODUCTS = WOMEN + MEN + UNISEX

PRICE_BUCKETS = ["low", "mid", "high"]
SEARCH_QUERIES = [
    "red dress", "summer skirt", "running shoes", "leather bag",
    "men watch", "office shirt", "heels", "wallet", "jacket", "sunglasses",
]
USER_SEGMENTS = ["browser", "buyer", "indecisive", "returner", "new_visitor"]
DEVICES = ["mobile", "desktop", "tablet"]


# ---------------------------------------------------------------------------
# Call builders
# ---------------------------------------------------------------------------
def call_products():
    return ("GET", "/products", "", "")


def call_products_id(pid):
    return ("GET", "/products", f"id={pid}", "")


def call_filter_open():
    return ("GET", "/products/filter", "", "")


def call_filter_apply(gender=None, category=None, price=None):
    body = {}
    if gender:
        body["gender"] = gender
    if category:
        body["category"] = category
    if price:
        body["price"] = price
    return ("POST", "/products/filter", "", json.dumps(body, separators=(",", ":")))


def call_cart(pid):
    return ("GET", "/cart", f"product_id={pid}", "")


def call_reviews(pid):
    return ("GET", "/reviews", f"product_id={pid}", "")


def call_wishlist_add(pid):
    return ("POST", "/wishlist", f"product_id={pid}", json.dumps({"product_id": pid}))


def call_search(q):
    return ("GET", "/search", f"q={q}", "")


def call_category(cat):
    return ("GET", "/category", f"type={cat}", "")


def call_home():
    return ("GET", "/home", "", "")


def call_checkout():
    return ("GET", "/checkout", "", "")


def call_payment():
    return ("POST", "/payment", "", json.dumps({"amount": 49.99, "method": "card"}))


def full_call(method, path, params, body):
    """Build the single 'api_call' string used as the LSTM token."""
    if params and body:
        return f"{method} {path}?{params}  body={body}"
    if params:
        return f"{method} {path}?{params}"
    if body:
        return f"{method} {path}  body={body}"
    return f"{method} {path}"


def make(method, path, params, body):
    return {
        "method": method,
        "endpoint": path,
        "params": params,
        "body": body,
        "call": full_call(method, path, params, body),
    }


# ---------------------------------------------------------------------------
# Session templates -- each returns a list of dicts produced by `make(...)`
# ---------------------------------------------------------------------------
def pick_product(matching=None):
    """Return (id, category, gender, price) optionally matching filter criteria."""
    pool = ALL_PRODUCTS
    if matching:
        pool = [
            p for p in ALL_PRODUCTS
            if all(p[i + 1] == v for i, v in enumerate(matching))
        ]
        if not pool:
            pool = ALL_PRODUCTS
    return random.choice(pool)


# --- Pattern 1: browse -> product -> cart (70%) or reviews (30%) -----------
def session_browse_simple():
    pid, *_ = pick_product()
    sequence = [make(*call_products())]
    sequence.append(make(*call_products_id(pid)))
    if random.random() < 0.70:
        sequence.append(make(*call_cart(pid)))
    else:
        sequence.append(make(*call_reviews(pid)))
    return sequence


# --- Pattern 2: filter page open -> filter apply -> product -> cart --------
def session_filter_to_cart():
    # User's exact example: gender=female, category=skirt -> id=44 -> cart
    gender = random.choice(["female", "male", "unisex"])
    category = random.choice(["skirt", "dress", "shirt", "sneakers", "watch"])
    price = random.choice(PRICE_BUCKETS + [None, None])  # price is optional

    pid, *_ = pick_product(
        matching=[gender, category, price] if price else [gender, category]
    )
    return [
        make(*call_filter_open()),
        make(*call_filter_apply(gender=gender, category=category, price=price)),
        make(*call_products_id(pid)),
        make(*call_cart(pid)),
    ]


# --- Pattern 3: filter -> product -> reviews --------------------------------
def session_filter_to_reviews():
    gender = random.choice(["female", "male"])
    category = random.choice(["skirt", "dress", "shirt", "sneakers"])
    pid, *_ = pick_product(matching=[gender, category])
    return [
        make(*call_filter_open()),
        make(*call_filter_apply(gender=gender, category=category)),
        make(*call_products_id(pid)),
        make(*call_reviews(pid)),
    ]


# --- Pattern 4: search -> product -> cart -----------------------------------
def session_search_to_cart():
    q = random.choice(SEARCH_QUERIES)
    pid, *_ = pick_product()
    return [
        make(*call_search(q)),
        make(*call_products_id(pid)),
        make(*call_cart(pid)),
    ]


# --- Pattern 5: search -> product -> reviews --------------------------------
def session_search_to_reviews():
    q = random.choice(SEARCH_QUERIES)
    pid, *_ = pick_product()
    return [
        make(*call_search(q)),
        make(*call_products_id(pid)),
        make(*call_reviews(pid)),
    ]


# --- Pattern 6: home -> category -> product -> cart -------------------------
def session_home_to_cart():
    cat = random.choice(["women", "men", "accessories"])
    gender = "female" if cat == "women" else "male" if cat == "men" else "unisex"
    pool = [p for p in ALL_PRODUCTS if p[2] == gender]
    pid, *_ = random.choice(pool)
    return [
        make(*call_home()),
        make(*call_category(cat)),
        make(*call_products_id(pid)),
        make(*call_cart(pid)),
    ]


# --- Pattern 7: browse -> multiple products -> cart -------------------------
def session_browse_multiple():
    n_views = random.randint(2, 4)
    pids = [pick_product()[0] for _ in range(n_views)]
    sequence = [make(*call_products())]
    for pid in pids:
        sequence.append(make(*call_products_id(pid)))
    final_pid = pids[-1]
    if random.random() < 0.65:
        sequence.append(make(*call_cart(final_pid)))
    else:
        sequence.append(make(*call_reviews(final_pid)))
    return sequence


# --- Pattern 8: browse -> product -> reviews -> back -> cart ----------------
def session_reviews_then_cart():
    pid, *_ = pick_product()
    return [
        make(*call_products()),
        make(*call_products_id(pid)),
        make(*call_reviews(pid)),
        make(*call_products_id(pid)),
        make(*call_cart(pid)),
    ]


# --- Pattern 9: product -> wishlist -> cart ---------------------------------
def session_wishlist_then_cart():
    pid, *_ = pick_product()
    return [
        make(*call_products()),
        make(*call_products_id(pid)),
        make(*call_wishlist_add(pid)),
        make(*call_cart(pid)),
    ]


# --- Pattern 10: cart -> checkout -> payment (buyer segment) ----------------
def session_cart_to_payment():
    pid, *_ = pick_product()
    return [
        make(*call_products()),
        make(*call_products_id(pid)),
        make(*call_cart(pid)),
        make(*call_checkout()),
        make(*call_payment()),
    ]


# --- Pattern 11: cart -> abandonment (browser segment) --------------------
def session_cart_abandon():
    pid, *_ = pick_product()
    return [
        make(*call_products()),
        make(*call_products_id(pid)),
        make(*call_cart(pid)),
        make(*call_home()),  # user leaves
    ]


# --- Pattern 12: filter returns no results -> re-filter -> product -> cart --
def session_filter_no_result():
    gender = random.choice(["female", "male"])
    bad_category = random.choice(["skirt", "sneakers", "watch"])
    good_pid, *_ = pick_product(matching=[gender, bad_category])
    return [
        make(*call_filter_open()),
        make(*call_filter_apply(gender=gender, category=bad_category, price="high")),
        make(*call_products()),  # empty results page
        make(*call_filter_apply(gender=gender, category=bad_category, price=None)),
        make(*call_products_id(good_pid)),
        make(*call_cart(good_pid)),
    ]


# --- Pattern 13: indecisive -- browse many products then cart ---------------
def session_indecisive():
    pids = [pick_product()[0] for _ in range(random.randint(3, 6))]
    seq = [make(*call_products())]
    for p in pids:
        seq.append(make(*call_products_id(p)))
        if random.random() < 0.3:
            seq.append(make(*call_reviews(p)))
    seq.append(make(*call_cart(pids[-1])))
    return seq


# --- Pattern 14: product -> reviews -> similar product -> cart --------------
def session_similar_product():
    pid, cat, *_ = pick_product()
    same_cat = [p for p in ALL_PRODUCTS if p[1] == cat and p[0] != pid]
    if not same_cat:
        same_cat = ALL_PRODUCTS
    other_pid, *_ = random.choice(same_cat)
    return [
        make(*call_products()),
        make(*call_products_id(pid)),
        make(*call_reviews(pid)),
        make(*call_products_id(other_pid)),
        make(*call_cart(other_pid)),
    ]


# --- Pattern 15: direct deep-link to product (e.g. ad click) ---------------
def session_direct_product():
    pid, *_ = pick_product()
    seq = [make(*call_products_id(pid))]
    if random.random() < 0.7:
        seq.append(make(*call_cart(pid)))
    else:
        seq.append(make(*call_reviews(pid)))
    return seq


# ---------------------------------------------------------------------------
# Pattern sampler (weighted)
# ---------------------------------------------------------------------------
PATTERNS = [
    (session_browse_simple,       22),  # the user's exact 70/30 cart/reviews case
    (session_filter_to_cart,      14),  # user's exact filter -> cart example
    (session_filter_to_reviews,    5),
    (session_search_to_cart,      10),
    (session_search_to_reviews,    4),
    (session_home_to_cart,         8),
    (session_browse_multiple,      8),
    (session_reviews_then_cart,    5),
    (session_wishlist_then_cart,   4),
    (session_cart_to_payment,      6),
    (session_cart_abandon,         5),
    (session_filter_no_result,     3),
    (session_indecisive,           4),
    (session_similar_product,      4),
    (session_direct_product,       8),
]


def weighted_choice():
    total = sum(w for _, w in PATTERNS)
    r = random.uniform(0, total)
    upto = 0
    for fn, w in PATTERNS:
        if upto + w >= r:
            return fn
        upto += w
    return PATTERNS[-1][0]


# ---------------------------------------------------------------------------
# Generate dataset
# ---------------------------------------------------------------------------
def generate(num_sessions=2000, out_path="synthetic_api_calls.csv"):
    rows = []
    for sid in range(1, num_sessions + 1):
        segment = random.choice(USER_SEGMENTS)
        device = random.choice(DEVICES)
        hour = random.randint(0, 23)

        pattern_fn = weighted_choice()
        sequence = pattern_fn()

        n = len(sequence)
        for i, step in enumerate(sequence):
            if i + 1 < n:
                nxt = sequence[i + 1]
                next_call = nxt["call"]
            else:
                # Session ends -> predict END token
                next_call = "END"
            rows.append({
                "session_id":   sid,
                "step":         i + 1,
                "current_call": step["call"],
                "next_call":    next_call,
                "endpoint":     step["endpoint"],
                "method":       step["method"],
                "url_params":   step["params"],
                "body":         step["body"],
                "user_segment": segment,
                "device":       device,
                "hour":         hour,
                "is_last_step": i + 1 == n,
            })

    fieldnames = [
        "session_id", "step", "current_call", "next_call",
        "endpoint", "method", "url_params", "body",
        "user_segment", "device", "hour", "is_last_step",
    ]
    out_file = Path(out_path)
    with out_file.open("w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()
        writer.writerows(rows)

    from collections import Counter
    ep_counter = Counter(r["endpoint"] for r in rows)
    next_counter = Counter(r["next_call"] for r in rows)
    print(f"Wrote {len(rows)} rows across {num_sessions} sessions -> {out_file.resolve()}")
    print(f"Unique current endpoints: {len(ep_counter)}")
    print(f"Top current endpoints:    {ep_counter.most_common(8)}")
    print(f"Top next_call tokens:     {next_counter.most_common(10)}")
    return rows


if __name__ == "__main__":
    generate(num_sessions=2000, out_path="synthetic_api_calls.csv")