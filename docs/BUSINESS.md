# Business workspace contract

The private `/business` tab manages this owner's rental invoices and incoming
payments. Every HTTP route below is under `/api/admin/business`, protected by
the existing ready-ADMIN middleware and scoped to the authenticated user ID.
Documents and bank credentials must never be public assets or Git fixtures.

## Data and accounting

All money is integer kopecks (`...Minor`), currency RUB. Dates are `YYYY-MM-DD`,
periods `YYYY-MM`, timestamps RFC3339. Quantities are decimal strings. Backend
calculates line amounts and totals using exact decimal arithmetic with half-up
rounding. Client totals are previews; server values are authoritative.

One invoice and its service act represent ONE receivable. Only `isCurrent=true`
invoices contribute to totals. Archived/replaced versions remain downloadable.
`templateId` distinguishes multiple contracts billed to one payer in a month. An
existing current `(counterpartyId, templateId, period)` returns 409 rather than
silently billing twice. Creating an invoice never sends it to the payer.

Payments are bank credits, deduplicated by account + provider transaction ID.
Their stable ID is `tochka-` plus the full lowercase SHA-256 hex digest of
`accountId + "\0" + transactionId` (fall back to paymentId only when transactionId
is absent). Private initial imports use the same identity as later bank sync.
An allocation applies an amount from one payment to one current invoice. Its
amount cannot exceed either remainder. Allocations, not coincident equal
monthly sums, establish invoice payment. Conservative automatic matching may
use a single explicit invoice-number reference AND exact payer INN AND invoice
year; ambiguous, third-party, overpaid or conflicting payments stay unallocated.
Manual allocation and reversal are available. No balance is called confirmed
debt when bank coverage or allocation is incomplete.

## Persistence and files

Use one typed workspace JSONB snapshot per authenticated owner and a separate
owner-scoped bytea document table. Mutations lock the workspace row in a SQL
transaction; file metadata and referenced content are validated. This is a
small personal ledger (hundreds of documents), not a general accounting engine.
Keep a `ponytail:` ceiling comment and a normalized-table upgrade point.

New local invoices are immutable downloadable A4 PDFs with Cyrillic support,
seller and buyer requisites, basis, items, dates, number and VAT label. A real
PDF library is appropriate; do not hand-roll PDF syntax. Historical original
PDF/DOCX/XLSX are preserved. No new facsimile signature is added by the service.

The optional bank token is AES-GCM encrypted at rest with a domain-separated
key derived from the existing high-entropy JWT secret, and owner-specific AAD.
Only connected/error metadata is returned. Tokens never enter logs, URLs or UI
storage. Changing a production secret/connection remains a separate owner
action; the feature works with imported documents/payments while disconnected.

## Shared JSON shapes

Use these camelCase names in Go DTOs and frontend types. Arrays are `[]`, not null.

```ts
type Contact = { email: string; note: string; confirmed: boolean };
type LineItem = { description: string; quantity: string; unit: string;
  priceMinor: number; amountMinor: number };
type InvoiceTemplate = { id: string; label: string; basis: string; items: LineItem[] };
type Seller = { name: string; inn: string; address: string; bankName: string;
  bankBik: string; bankAccount: string; bankCorrespondentAccount: string };
type Counterparty = { id: string; name: string; entityType: 'company'|'ip'|'person';
  inn: string; address: string; contacts: Contact[]; templates: InvoiceTemplate[] };
type Document = { id: string; kind: 'invoice'|'act'|'other'; filename: string;
  mimeType: string; size: number; sha256: string };
type Invoice = { id: string; counterpartyId: string; templateId: string;
  number: string; issueDate: string; dueDate: string; period: string;
  periodStart: string; periodEnd: string; basis: string; items: LineItem[];
  amountMinor: number; currency: 'RUB'; vat: string;
  isCurrent: boolean; revision: number; source: string; provider: 'local'|'tochka';
  providerState: string; bankDocumentId: string; documents: Document[];
  createdAt: string };
type Allocation = { id: string; invoiceId: string; paymentId: string;
  amountMinor: number; automatic: boolean };
type Payment = { id: string; date: string; amountMinor: number; currency: 'RUB';
  counterpartyId: string; counterpartyName: string; payerInn: string;
  description: string; accountLabel: string; source: string };
type Bank = { connected: boolean; lastSyncedAt: string; coverageStart: string;
  coverageEnd: string; accountCount: number; error: string };
type InvoiceView = Invoice & { paidMinor: number; outstandingMinor: number;
  status: 'paid'|'partial'|'unmatched'|'unreviewed'|'archived'; allocations: Allocation[] };
type PaymentView = Payment & { allocatedMinor: number; unallocatedMinor: number;
  allocations: Allocation[] };
type CounterpartyView = Counterparty & { invoicedMinor: number; paidMinor: number;
  outstandingMinor: number; receivedMinor: number; invoiceCount: number;
  unreviewedCount: number; lastPaymentAt: string };
type Summary = { invoicedMinor: number; paidMinor: number; outstandingMinor: number;
  receivedMinor: number; unallocatedMinor: number; invoiceCount: number;
  counterpartyCount: number; paidInvoiceCount: number; partialInvoiceCount: number;
  unreviewedInvoiceCount: number; reconciliationComplete: boolean };
type Workspace = { seller: Seller; counterparties: CounterpartyView[];
  invoices: InvoiceView[]; payments: PaymentView[]; summary: Summary;
  bank: Bank; periods: string[]; nextInvoiceNumber: string };
```

`paidMinor` is allocated to the selected invoice period, regardless of payment
date. `receivedMinor` is credits posted during the selected calendar period.
They must not be presented as interchangeable. `outstandingMinor` is the
unallocated invoice remainder, not automatically confirmed overdue debt.

`unreviewed` means no usable statement coverage yet. `unmatched` means coverage
exists but no allocated payment was found; it does not claim legally overdue
debt. `archived` never contributes to headline totals. Payer history can include
archived rows while keeping them visibly distinct.

## HTTP endpoints

* `GET /?period=YYYY-MM|all&counterpartyId=...` -> `Workspace` (query filters
  invoice period and optionally payer; omitted period means all). Return all
  payer metadata for create controls; summaries include only the selected scope.
* `POST /invoices` -> 201 `InvoiceView`. Body `{ counterpartyId, templateId,
  period, issueDate, dueDate?, number?, basis?, items, provider }`. Default provider
  local. Number may be empty for atomic next-number allocation. Reject person
  for Tochka. Local invoice creation works without bank connection. Store local
  bank-create intent BEFORE the external call; never retry an uncertain bank
  write automatically. An uncertain result blocks duplicate creation and is
  visible as `providerState='unknown'`, pending review.
* `GET /files/{id}` -> authenticated attachment with correct MIME and safe
  Content-Disposition. No public/raw filesystem paths.
* `POST /allocations` body `{invoiceId,paymentId,amountMinor}` -> 201 allocation;
  `DELETE /allocations/{id}` -> 204. Return workspace after re-fetch.
* `POST /bank/connect` body `{token}` -> `Bank`. Validate with account read,
  encrypt and persist, clear request token from response. `DELETE /bank/connect`
  -> 204 removes only stored connection, preserving imported payments/history.
* `POST /bank/sync` body `{from,to}` -> `Bank` after bounded synchronous statement
  retrieval for all accessible RUB accounts. Repeat is idempotent. Failed or
  partial retrieval must not mark complete coverage. Never call outgoing payment
  APIs. No automatic background schedule is created.
* `POST /import` -> `{counterpartyCount,invoiceCount,paymentCount,documentCount}`.
  Protected bounded JSON body `{seller,counterparties,invoices,payments?,bank?,
  documents:[{id,filename,mimeType,dataBase64}]}`. Idempotent merge by stable IDs;
  do not erase existing payments, allocations or bank token. Recompute document
  hashes/size and all money totals; validate every reference. This endpoint is
  for initial private archive import, not a public uploader.

Errors: `{message:string}` with 400 validation, 404 owner-scoped unknown ID,
409 duplicates/allocation conflicts, 413 limits, 502 uncertain bank failure and 503 bank
not configured. Never echo token or upstream private response bodies.
An explicit rejected bank invoice is a safe 400 error; its archived intent does
not prevent creating a local PDF. An uncertain bank write remains blocked for
review rather than being retried.

Read-only bank access first uses the native REST API. When the token is refused
with 401/403, use the official fixed-host MCP endpoint for account/statement
reads. LLM keys can expose these reads without native invoice permissions. The
MCP path never calls invoice creation or outgoing-payment tools; a local PDF
remains available when bank invoice creation is unavailable.
Statement generation is asynchronous at the bank. Initialize reports for all
accessible accounts before polling, within the single 90-second read budget.
An unfinished report leaves the previous coverage unchanged.

## Source/import contract

The private export is NOT committed. It imports canonical monthly invoices
without charging separately for their service acts. Append-only archive
packets include revisions and overlapping cumulative bills. Keep their unique
invoice/act files available as historical invoice records with
`isCurrent=false`; do not add cumulative overlaps as debt.
Exclude handover acts from recurring receivables. Reuse identical file bytes
by SHA while keeping source provenance in history. No contracts, passports,
registry snapshots, standalone signatures, or token files are imported.

## Verification

Focused tests must cover owner isolation/admin routes, integer rounding,
duplicate/current invoice rejection, historical versions excluded from totals,
idempotent transaction import, partial/multiple allocation limits and reversal,
unknown payment coverage, PDF Cyrillic output, token redaction/encryption, and
bank-write uncertain-result retry refusal. Bank tests use an injected HTTP
fake; no tests call real bank services. Run both repository gates and browser
flows at desktop, tablet and phone before publication.
