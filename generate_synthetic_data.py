"""
Synthetic e-commerce API session generator for Next API Call Prediction.

IMPORTANT: every emitted token MUST match exactly what the Go middleware
records in session history:  "METHOD /path?query"  (RequestURI, no body).
Bodies are never visible to the predictor, so POST tokens carry no body.

The funnel is intentionally dominant: if a session reaches GET /checkout the
next call is POST /payment with very high probability, so the trained model
"memorizes" the logical commerce flow (cart -> checkout -> payment).

Outputs synthetic_api_calls.csv with (session, step, current_call, next_call).
"""

import csv
import random
from pathlib import Path

random.seed(1337)

# Real catalogue IDs (matches Supabase data / frontend).
PRODUCT_IDS = [11, 15, 22, 33, 39, 42, 44, 47, 55, 61, 73, 88, 101, 105, 110, 120]

END = "END"


# ---------------------------------------------------------------------------
# Token builders â€” format is load-bearing, do not change lightly.
# Only /products and /reviews carry product ids; everything else is a bare
# path, matching the middleware's history sanitization.
# ---------------------------------------------------------------------------
def t_products():
    return "GET /products"


def t_product(pid):
    return f"GET /products?id={pid}"


def t_cart_add():
    return "POST /cart"


def t_cart_view():
    return "GET /cart"


def t_checkout():
    return "GET /checkout"


def t_payment():
    return "POST /payment"


def t_wishlist_add():
    return "POST /wishlist"


def t_wishlist_view():
    return "GET /wishlist"


def t_reviews(pid):
    return f"GET /reviews?product_id={pid}"


def t_home():
    return "GET /home"


def t_search():
    return "GET /search"


def t_category():
    return "GET /category"


def t_filter_apply():
    return "POST /products/filter"


# ---------------------------------------------------------------------------
# Funnel helpers
# ---------------------------------------------------------------------------
def pick_pid():
    return random.choice(PRODUCT_IDS)


def funnel_tail(pay_prob=0.95, abandon_prob=0.15):
    """GET /cart onward. Returns list of tokens ending with payment or leave."""
    tail = []
    if random.random() < abandon_prob:
        tail.append(t_home())  # user leaves via home
        return tail
    tail.append(t_checkout())
    if random.random() < pay_prob:
        tail.append(t_payment())
    return tail


def buy_step(pid):
    return [t_cart_add(), t_cart_view()]


# ---------------------------------------------------------------------------
# Session patterns â€” weighted toward the logical commerce funnel
# ---------------------------------------------------------------------------
def s_buyer_full():
    """browse -> product -> add to cart -> cart -> checkout -> payment"""
    pid = pick_pid()
    seq = [t_products(), t_product(pid)]
    seq += buy_step(pid)
    seq += funnel_tail()
    return seq


def s_buyer_reviews():
    """browse -> product -> reviews -> add to cart -> ... -> payment"""
    pid = pick_pid()
    seq = [t_products(), t_product(pid), t_reviews(pid)]
    seq += buy_step(pid)
    seq += funnel_tail()
    return seq


def s_buyer_browses_many():
    """browse -> few products -> buy last one"""
    seq = [t_products()]
    for _ in range(random.randint(2, 3)):
        seq.append(t_product(pick_pid()))
    pid = seq[-1].split("id=")[1]
    seq += buy_step(int(pid))
    seq += funnel_tail()
    return seq


def s_wishlist_save():
    """product -> save to wishlist -> view wishlist"""
    pid = pick_pid()
    seq = [t_products(), t_product(pid), t_wishlist_add(), t_wishlist_view()]
    if random.random() < 0.35:  # saved it, comes back and buys it
        seq += buy_step(pid)
        seq += funnel_tail()
    return seq


def s_wishlist_then_buy():
    """product -> save -> (no view) -> buy it anyway"""
    pid = pick_pid()
    seq = [t_products(), t_product(pid), t_wishlist_add()]
    seq += buy_step(pid)
    seq += funnel_tail()
    return seq


def s_home_funnel():
    """home -> category -> product -> buy"""
    seq = [t_home(), t_category()]
    pid = pick_pid()
    seq.append(t_product(pid))
    seq += buy_step(pid)
    seq += funnel_tail()
    return seq


def s_search_funnel():
    """search -> product -> buy"""
    seq = [t_search()]
    pid = pick_pid()
    seq.append(t_product(pid))
    seq += buy_step(pid)
    seq += funnel_tail()
    return seq


def s_filter_funnel():
    """products -> apply filter -> product -> buy"""
    seq = [t_products(), t_filter_apply()]
    pid = pick_pid()
    seq.append(t_product(pid))
    seq += buy_step(pid)
    seq += funnel_tail()
    return seq


def s_abandon_cart():
    """gets to the cart then leaves"""
    pid = pick_pid()
    return [t_products(), t_product(pid), t_cart_add(), t_cart_view(), t_home()]


def s_window_shopper():
    """products + reviews, never buys"""
    seq = [t_products()]
    for _ in range(random.randint(1, 3)):
        pid = pick_pid()
        seq.append(t_product(pid))
        if random.random() < 0.6:
            seq.append(t_reviews(pid))
    if random.random() < 0.4:
        seq.append(t_home())
    return seq


def s_direct_deeplink():
    """ad click straight into a product, then buys"""
    pid = pick_pid()
    seq = [t_product(pid)]
    seq += buy_step(pid)
    seq += funnel_tail()
    return seq


def s_cart_rebuy():
    """back to an existing cart, straight to checkout"""
    seq = [t_cart_view(), t_checkout()]
    if random.random() < 0.9:
        seq.append(t_payment())
    return seq


def s_direct_checkout():
    """deep-links straight into the checkout tab, then pays"""
    seq = [t_checkout()]
    if random.random() < 0.9:
        seq.append(t_payment())
    return seq


PATTERNS = [
    (s_buyer_full,         26),  # the dominant logical funnel
    (s_buyer_reviews,      12),
    (s_buyer_browses_many, 10),
    (s_wishlist_save,       8),
    (s_wishlist_then_buy,   6),
    (s_home_funnel,        10),
    (s_search_funnel,      10),
    (s_filter_funnel,       8),
    (s_abandon_cart,        4),
    (s_window_shopper,      4),
    (s_direct_deeplink,     6),
    (s_cart_rebuy,          4),
    (s_direct_checkout,     8),
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


def generate(num_sessions=5000, out_path="synthetic_api_calls.csv"):
    rows = []
    for sid in range(1, num_sessions + 1):
        seq = weighted_choice()()[:8]  # MAX_LEN is 8; keep sessions within it
        n = len(seq)
        for i, call in enumerate(seq):
            nxt = seq[i + 1] if i + 1 < n else END
            rows.append({
                "session_id": sid,
                "step": i + 1,
                "current_call": call,
                "next_call": nxt,
                "is_last_step": i + 1 == n,
            })

    fieldnames = ["session_id", "step", "current_call", "next_call", "is_last_step"]
    with Path(out_path).open("w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()
        writer.writerows(rows)

    from collections import Counter
    vocab = set(r["current_call"] for r in rows) | set(r["next_call"] for r in rows)
    after_checkout = Counter(
        r["next_call"] for r in rows if r["current_call"] == t_checkout()
    )
    after_cart = Counter(
        r["next_call"] for r in rows if r["current_call"] == t_cart_view()
    )
    print(f"Wrote {len(rows)} rows across {num_sessions} sessions -> {Path(out_path).resolve()}")
    print(f"Vocab size: {len(vocab)}")
    print(f"After {t_checkout()}: {dict(after_checkout.most_common(4))}")
    print(f"After {t_cart_view()}: {dict(after_cart.most_common(4))}")
    return rows


if __name__ == "__main__":
    generate(num_sessions=5000, out_path="synthetic_api_calls.csv")
