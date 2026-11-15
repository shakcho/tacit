import { LogOut, ShieldCheck } from "lucide-react";
import { withAuth } from "@workos-inc/authkit-nextjs";

import { buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { PageHeader } from "@/components/ui/page-header";

export default async function ProfilePage() {
  const auth = await withAuth({ ensureSignedIn: true });
  return (
    <div className="space-y-8">
      <PageHeader description="Your WorkOS session identifies you; Tacit memberships and roles decide workspace access." eyebrow="Identity" title="Account" />
      <Card className="max-w-2xl">
        <CardHeader>
          <div>
            <h2 className="flex items-center gap-2 font-medium"><ShieldCheck aria-hidden="true" className="size-5 text-primary" /> Managed identity</h2>
            <p className="mt-2 text-sm leading-6 text-muted-foreground">Session credentials are kept in an encrypted, HttpOnly cookie and are never stored in browser application state.</p>
          </div>
        </CardHeader>
        <CardContent className="space-y-5">
          <dl className="grid gap-4 text-sm sm:grid-cols-2">
            <div><dt className="text-muted-foreground">Email</dt><dd className="mt-1 font-medium">{auth.user.email}</dd></div>
            <div><dt className="text-muted-foreground">WorkOS organization</dt><dd className="mt-1 font-medium">{auth.organizationId ?? "Not selected"}</dd></div>
          </dl>
          <form action="/auth/sign-out" method="post">
            <button className={buttonVariants({ variant: "secondary" })} type="submit">
              <LogOut aria-hidden="true" className="size-4" /> Sign out
            </button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
