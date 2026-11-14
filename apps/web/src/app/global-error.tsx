"use client";

import { Button } from "@/components/ui/button";

type GlobalErrorProps = {
  reset: () => void;
};

export default function GlobalError({ reset }: GlobalErrorProps) {
  return (
    <html lang="en">
      <body>
        <main className="flex min-h-screen items-center justify-center bg-background px-6 text-foreground">
          <div className="max-w-lg text-center">
            <h1 className="text-4xl font-medium tracking-[-0.04em]">Tacit could not load</h1>
            <p className="mt-4 text-base leading-7 text-muted-foreground">Try the request again. Your meeting data has not been changed.</p>
            <Button className="mt-7" onClick={reset}>Try again</Button>
          </div>
        </main>
      </body>
    </html>
  );
}
