import { AudioLines, Captions, Sparkles, Upload, Video, type LucideIcon } from "lucide-react";
import Link from "next/link";

import { Badge } from "@/components/ui/badge";
import { buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { PageHeader } from "@/components/ui/page-header";

const stages = [
  {
    description: "Incoming audio and video will appear here after a meeting source is connected.",
    icon: AudioLines,
    title: "Capture",
  },
  {
    description: "Partial and final transcripts will keep speaker and recording timestamps together.",
    icon: Captions,
    title: "Transcript",
  },
  {
    description: "Accepted highlights and playlists will remain editable product records.",
    icon: Sparkles,
    title: "Review",
  },
] satisfies readonly { description: string; icon: LucideIcon; title: string }[];

export default function MeetingsPage() {
  return (
    <div className="space-y-8" data-enter>
      <PageHeader
        action={
          <Link className={buttonVariants({ variant: "primary" })} href="/meetings/new">
            <Upload aria-hidden="true" className="size-4" />
            Upload a recording
          </Link>
        }
        description="Review recordings, speaker-aware transcripts, highlights, and playlists from one meeting record."
        eyebrow="Meeting workspace"
        title="Meeting intelligence workspace"
      />

      <section aria-labelledby="workflow-title">
        <div className="mb-4 flex items-center justify-between gap-4">
          <h2 className="text-lg font-medium tracking-tight" id="workflow-title">Recording workflow</h2>
          <Badge>3 processing stages</Badge>
        </div>
        <div className="grid gap-4 lg:grid-cols-3">
          {stages.map((stage) => {
            const Icon = stage.icon;
            return (
              <Card className="min-h-40" key={stage.title}>
                <CardHeader>
                  <div>
                    <span className="mb-4 inline-flex size-9 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                      <Icon aria-hidden="true" className="size-[18px]" strokeWidth={1.8} />
                    </span>
                    <h3 className="text-base font-medium">{stage.title}</h3>
                  </div>
                </CardHeader>
                <CardContent>
                  <p className="text-sm leading-6 text-muted-foreground">{stage.description}</p>
                </CardContent>
              </Card>
            );
          })}
        </div>
      </section>

      <section aria-labelledby="recent-meetings-title">
        <h2 className="mb-4 text-lg font-medium tracking-tight" id="recent-meetings-title">Recent meetings</h2>
        <EmptyState
          action={
            <Link className={buttonVariants({ variant: "secondary" })} href="/meetings/new">
              <Video aria-hidden="true" className="size-4" />
              Add a meeting
            </Link>
          }
          description="Connect a meeting source or upload a recording. Tacit will keep capture, transcript, and review states visible here."
          icon={Video}
          title="No meetings yet"
        />
      </section>
    </div>
  );
}
