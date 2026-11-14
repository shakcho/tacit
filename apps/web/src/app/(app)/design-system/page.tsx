import { Inbox, Upload } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { PageHeader } from "@/components/ui/page-header";

export default function DesignSystemPage() {
  return (
    <div className="space-y-10" data-enter>
      <PageHeader
        description="Owned interface primitives, semantic tokens, route states, and responsive behavior for the Tacit product workspace."
        eyebrow="Component review"
        title="Tacit design system"
      />

      <section aria-labelledby="actions-heading">
        <h2 className="text-xl font-medium tracking-tight" id="actions-heading">Actions and states</h2>
        <div className="mt-5 flex flex-wrap items-center gap-3 rounded-xl border border-border bg-card p-6">
          <Button><Upload aria-hidden="true" className="size-4" />Upload a recording</Button>
          <Button variant="secondary">See meetings</Button>
          <Button variant="ghost">Dismiss</Button>
          <Badge tone="positive">Ready</Badge>
          <Badge tone="attention">Needs review</Badge>
          <Badge>Queued</Badge>
        </div>
      </section>

      <section aria-labelledby="cards-heading">
        <h2 className="text-xl font-medium tracking-tight" id="cards-heading">Cards and empty content</h2>
        <div className="mt-5 grid gap-5 lg:grid-cols-2">
          <Card>
            <CardHeader>
              <div>
                <p className="text-sm text-muted-foreground">Transcript</p>
                <h3 className="mt-1 text-lg font-medium">Speaker-aware text</h3>
              </div>
              <Badge tone="positive">Ready</Badge>
            </CardHeader>
            <CardContent>
              <p className="text-sm leading-6 text-muted-foreground">Final words, speaker labels, and recording timestamps stay on the same revision.</p>
            </CardContent>
          </Card>
          <EmptyState
            description="The same component handles new workspaces and filtered results with direct, specific language."
            icon={Inbox}
            title="No items to review"
          />
        </div>
      </section>
    </div>
  );
}
