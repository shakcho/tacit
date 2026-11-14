import {
  ArrowUpRight,
  AudioLines,
  Captions,
  ChevronRight,
  CircleCheck,
  Clock3,
  MessagesSquare,
  Mic2,
  MonitorUp,
  Search,
  Sparkles,
  Upload,
  Users,
  Video,
  WandSparkles,
  type LucideIcon,
} from "lucide-react";
import Link from "next/link";

import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/cn";

type Meeting = {
  date: string;
  duration: string;
  icon: LucideIcon;
  project: string;
  source: string;
  status: "Processing" | "Ready" | "Reviewed";
  title: string;
};

const metrics = [
  { detail: "2 new today", label: "Ready to review", value: "4" },
  { detail: "across 6 speakers", label: "Highlights found", value: "17" },
  { detail: "38 min remaining", label: "Processing", value: "2" },
] as const;

const meetings = [
  {
    date: "Today, 9:30 AM",
    duration: "48 min",
    icon: Video,
    project: "Northstar research",
    source: "Zoom",
    status: "Ready",
    title: "Customer advisory council",
  },
  {
    date: "Yesterday, 3:15 PM",
    duration: "32 min",
    icon: MonitorUp,
    project: "Activation study",
    source: "Microsoft Teams",
    status: "Processing",
    title: "Onboarding research synthesis",
  },
  {
    date: "Nov 12, 11:00 AM",
    duration: "56 min",
    icon: Video,
    project: "Product weekly",
    source: "Zoom",
    status: "Reviewed",
    title: "Product and design sync",
  },
  {
    date: "Nov 11, 1:45 PM",
    duration: "41 min",
    icon: Upload,
    project: "Partner program",
    source: "Upload",
    status: "Ready",
    title: "Implementation partner kickoff",
  },
] satisfies readonly Meeting[];

const transcript = [
  {
    initials: "MK",
    name: "Maya Kim",
    text: "The summary is useful, but I still need to jump to the exact moment a customer explains why the workflow broke.",
    time: "18:42",
  },
  {
    initials: "JL",
    name: "Jon Bell",
    text: "If the evidence and the clip stay together, I can share it with engineering without rewriting the customer’s point.",
    time: "19:08",
  },
] as const;

function statusStyle(status: Meeting["status"]): string {
  if (status === "Ready") return "text-primary";
  if (status === "Processing") return "text-amber-700";
  return "text-emerald-700";
}

export default function MeetingsPage() {
  return (
    <div className="space-y-7" data-enter>
      <header className="grid gap-7 border-b border-border pb-7 xl:grid-cols-[minmax(0,1fr)_auto] xl:items-end">
        <div>
          <p className="mb-3 text-sm font-medium text-muted-foreground">Demo workspace</p>
          <h1 className="max-w-4xl text-balance text-[clamp(2.6rem,5vw,5.2rem)] font-medium leading-[0.94] tracking-[-0.065em]">
            Meetings, ready to review.
          </h1>
          <p className="mt-4 max-w-2xl text-pretty text-base leading-7 text-muted-foreground sm:text-lg">
            Turn every conversation into searchable evidence, accepted highlights, and playlists your team can act on.
          </p>
        </div>

        <div className="grid grid-cols-3 divide-x divide-border xl:min-w-[32rem]">
          {metrics.map((metric) => (
            <div className="px-4 first:pl-0 xl:px-6 xl:last:pr-0" key={metric.label}>
              <p className="tabular-nums text-3xl font-medium tracking-[-0.04em] sm:text-4xl">{metric.value}</p>
              <p className="mt-2 text-sm font-medium">{metric.label}</p>
              <p className="mt-1 hidden text-xs text-muted-foreground sm:block">{metric.detail}</p>
            </div>
          ))}
        </div>
      </header>

      <section
        aria-labelledby="assistant-title"
        className="overflow-hidden rounded-2xl border border-primary/20 bg-card shadow-[0_1.5rem_4rem_oklch(0.55_0.12_264/0.08)]"
      >
        <div className="grid gap-6 p-5 sm:p-7 lg:grid-cols-[auto_minmax(0,1fr)] lg:items-start">
          <div className="flex items-center gap-4 lg:items-start">
            <span aria-hidden="true" className="assistant-orb size-12 shrink-0 rounded-full lg:size-16" />
            <div className="lg:min-w-44">
              <h2 className="text-xl font-semibold tracking-[-0.03em]" id="assistant-title">Tacit AI</h2>
              <p className="text-sm text-muted-foreground">Your meeting analyst</p>
            </div>
          </div>

          <div>
            <Link
              className="flex min-h-12 w-full items-center gap-3 rounded-xl border border-border bg-background px-4 text-sm text-muted-foreground transition-[border-color,box-shadow] hover:border-primary/35 hover:shadow-[0_0_0_3px_oklch(0.56_0.19_264/0.08)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              href="/search"
            >
              <Search aria-hidden="true" className="size-[18px]" strokeWidth={1.8} />
              <span>Ask about a meeting, customer, or theme…</span>
              <span className="ml-auto inline-flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
                <Mic2 aria-hidden="true" className="size-4" />
              </span>
            </Link>

            <p className="mt-5 max-w-4xl text-pretty text-lg leading-8 sm:text-xl">
              Two conversations need review. I found <span className="font-semibold text-primary">17 evidence-backed highlights</span>, including a repeated request to keep customer quotes linked to the exact recording moment.
            </p>

            <div className="mt-5 flex flex-wrap gap-2">
              {["Show recurring objections", "What changed this week?", "Draft a research playlist"].map((prompt) => (
                <Link className="rounded-lg border border-border px-3.5 py-2 text-sm text-muted-foreground transition-colors hover:border-primary/30 hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" href="/search" key={prompt}>
                  {prompt}
                </Link>
              ))}
            </div>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-t border-border bg-muted/45 px-5 py-3 text-xs text-muted-foreground sm:px-7">
          <span className="inline-flex items-center gap-2 font-semibold text-primary">
            <span aria-hidden="true" className="size-2 rounded-full bg-primary shadow-[0_0_0_4px_oklch(0.56_0.19_264/0.12)]" />
            DEMO DATA
          </span>
          <span>Transcript indexed 2 min ago</span>
          <span className="ml-auto inline-flex items-center gap-2">
            <Video aria-hidden="true" className="size-4" /> Zoom sample
            <MonitorUp aria-hidden="true" className="ml-2 size-4" /> Teams sample
            <Upload aria-hidden="true" className="ml-2 size-4" /> Uploads
          </span>
        </div>
      </section>

      <section aria-labelledby="review-queue-title" className="overflow-hidden rounded-2xl border border-border bg-card">
        <div className="grid min-h-[38rem] lg:grid-cols-[minmax(18rem,0.72fr)_minmax(0,1.28fr)]">
          <div className="border-b border-border lg:border-b-0 lg:border-r">
            <div className="border-b border-border px-5 pt-5 sm:px-6">
              <div className="flex items-center justify-between gap-4">
                <h2 className="text-lg font-semibold tracking-[-0.025em]" id="review-queue-title">Review queue</h2>
                <Link className="inline-flex min-h-10 items-center gap-2 rounded-lg px-3 text-sm font-medium text-primary hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" href="/meetings/new">
                  <Upload aria-hidden="true" className="size-4" /> Add
                </Link>
              </div>
              <div className="mt-4 flex gap-5 text-sm">
                <span className="border-b-2 border-foreground pb-3 font-medium">Needs review <span className="ml-1 text-muted-foreground">4</span></span>
                <span className="pb-3 text-muted-foreground">All meetings <span className="ml-1">12</span></span>
              </div>
            </div>

            <ol aria-label="Meetings awaiting review" className="divide-y divide-border">
              {meetings.map((meeting, index) => {
                const Icon = meeting.icon;
                return (
                  <li className={cn("relative p-5 sm:p-6", index === 0 && "bg-muted/65 before:absolute before:inset-y-0 before:left-0 before:w-1 before:bg-primary")} key={meeting.title}>
                    <article>
                      <div className="flex items-center gap-2 text-xs text-muted-foreground">
                        <Icon aria-hidden="true" className="size-4" strokeWidth={1.8} />
                        <span>{meeting.source}</span>
                        <span aria-hidden="true">·</span>
                        <span className={statusStyle(meeting.status)}>{meeting.status}</span>
                      </div>
                      <h3 className="mt-3 text-base font-medium tracking-[-0.02em]">{meeting.title}</h3>
                      <p className="mt-1 text-sm text-muted-foreground">{meeting.project}</p>
                      <div className="mt-4 flex items-center justify-between text-xs text-muted-foreground">
                        <span>{meeting.date}</span>
                        <span className="tabular-nums">{meeting.duration}</span>
                      </div>
                    </article>
                  </li>
                );
              })}
            </ol>
          </div>

          <article className="flex min-w-0 flex-col" id="selected-meeting">
            <div className="border-b border-border p-5 sm:p-7">
              <div className="flex flex-wrap items-start justify-between gap-4">
                <div>
                  <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <span>Northstar research</span>
                    <span aria-hidden="true">·</span>
                    <span className="text-primary">Ready to review</span>
                  </div>
                  <h2 className="mt-2 text-balance text-2xl font-semibold tracking-[-0.04em] sm:text-3xl">Customer advisory council</h2>
                </div>
                <Link className={buttonVariants({ size: "sm", variant: "secondary" })} href="/highlights">
                  Review highlights <ChevronRight aria-hidden="true" className="size-4" />
                </Link>
              </div>

              <div className="mt-5 flex flex-wrap items-center gap-x-5 gap-y-3 text-sm text-muted-foreground">
                <span className="inline-flex items-center gap-2"><Video aria-hidden="true" className="size-4" /> Zoom</span>
                <span className="inline-flex items-center gap-2"><Clock3 aria-hidden="true" className="size-4" /> 48 min</span>
                <span className="inline-flex items-center gap-2"><Users aria-hidden="true" className="size-4" /> 6 speakers</span>
                <span className="inline-flex items-center gap-2"><Captions aria-hidden="true" className="size-4" /> Diarized</span>
              </div>
            </div>

            <div className="grid flex-1 gap-7 p-5 sm:p-7 xl:grid-cols-[minmax(0,1fr)_17rem]">
              <div className="min-w-0">
                <section aria-labelledby="summary-title">
                  <div className="flex items-center gap-2">
                    <WandSparkles aria-hidden="true" className="size-[18px] text-primary" />
                    <h3 className="font-semibold" id="summary-title">What mattered</h3>
                  </div>
                  <p className="mt-3 max-w-3xl text-pretty text-[0.9375rem] leading-7 text-muted-foreground">
                    Customers trust generated summaries when every claim links back to the recording. Three participants asked for a faster way to package related moments for product and engineering teams.
                  </p>
                </section>

                <section aria-labelledby="transcript-title" className="mt-8">
                  <div className="flex items-center justify-between gap-4">
                    <div className="flex items-center gap-2">
                      <AudioLines aria-hidden="true" className="size-[18px] text-primary" />
                      <h3 className="font-semibold" id="transcript-title">Transcript evidence</h3>
                    </div>
                    <Link className="text-sm font-medium text-primary hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" href="/search">Search transcript</Link>
                  </div>

                  <div className="mt-4 space-y-3">
                    {transcript.map((line) => (
                      <blockquote className="rounded-xl bg-muted/65 p-4" key={line.time}>
                        <div className="flex gap-3">
                          <span aria-hidden="true" className="inline-flex size-8 shrink-0 items-center justify-center rounded-lg bg-foreground text-[0.6875rem] font-semibold text-background">{line.initials}</span>
                          <div>
                            <div className="flex items-baseline gap-2 text-sm">
                              <cite className="not-italic font-medium">{line.name}</cite>
                              <span className="tabular-nums text-xs text-muted-foreground">{line.time}</span>
                            </div>
                            <p className="mt-1.5 text-sm leading-6 text-muted-foreground">“{line.text}”</p>
                          </div>
                        </div>
                      </blockquote>
                    ))}
                  </div>
                </section>
              </div>

              <aside aria-labelledby="insights-title" className="rounded-xl bg-muted/55 p-5">
                <div className="flex items-center gap-2">
                  <Sparkles aria-hidden="true" className="size-[18px] text-primary" />
                  <h3 className="font-semibold" id="insights-title">Suggested insights</h3>
                </div>
                <ul className="mt-4 space-y-4 text-sm">
                  <li className="flex gap-3"><CircleCheck aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-emerald-600" /><span>Evidence links increase trust in AI summaries.</span></li>
                  <li className="flex gap-3"><MessagesSquare aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-primary" /><span>Teams want shareable bundles, not isolated clips.</span></li>
                  <li className="flex gap-3"><Captions aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-primary" /><span>Speaker labels need to remain editable after processing.</span></li>
                </ul>
                <Link className="mt-6 inline-flex items-center gap-1 text-sm font-medium text-primary hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" href="/highlights">
                  Open all 7 highlights <ArrowUpRight aria-hidden="true" className="size-4" />
                </Link>
              </aside>
            </div>
          </article>
        </div>
      </section>
    </div>
  );
}
