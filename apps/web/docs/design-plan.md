# Tacit product-shell plan

Kind: software — a meeting intelligence workspace for teams reviewing recordings and transcript evidence.

Deal: a cool-white workspace, near-black ink, and one cobalt action colour; self-hosted Geist Sans; restrained radius; solid actions; a fixed light desktop rail and compact mobile action bar. Large editorial headings, inline operational metrics, and master-detail work surfaces create clear hierarchy without turning the product into a generic card grid. The local font keeps builds deterministic.

Voice: technical-direct. Exact, not cold; short labels; the product is the subject; no jokes or invented proof.

- `/meetings`: product shell and realistic demo workspace; an assistant surface explains what needs attention, while the review queue and selected meeting form one responsive master-detail screen. Planned providers must be labelled as planned or sample data until their connector tickets ship.
- `/share/[shareId]`: separate anonymous surface with explicit shared-view and pending capability state.
- `/auth/callback`: narrow identity-provider boundary state with no local authentication behavior.
- `/design-system`: owned component review for actions, statuses, cards, and empty content.
- Compatibility routes: server redirects only; no legacy UI or token behavior enters the new application.

Primary action: Upload a recording. The full recording workflow remains out of scope until its product ticket.
