import type { Metadata, Viewport } from "next";
import localFont from "next/font/local";
import type { ReactNode } from "react";
import { AuthKitProvider } from "@workos-inc/authkit-nextjs/components";

import { QueryProvider } from "@/components/query-provider";

import "./globals.css";

const geistSans = localFont({
  display: "swap",
  src: "./fonts/Geist-Variable.woff2",
  variable: "--font-geist-sans",
  weight: "100 900",
});

export const metadata: Metadata = {
  description: "A workspace for reviewing meeting recordings, transcripts, highlights, and playlists.",
  openGraph: {
    description: "Review meeting recordings, transcripts, highlights, and playlists in one workspace.",
    siteName: "Tacit",
    title: "Tacit meeting workspace",
    type: "website",
  },
  other: {
    "page-type": "software",
    "site-kind": "software",
  },
  title: {
    default: "Tacit meeting workspace",
    template: "%s · Tacit",
  },
};

export const viewport: Viewport = {
  colorScheme: "light",
  themeColor: "#f8f6f1",
  width: "device-width",
};

type RootLayoutProps = {
  children: ReactNode;
};

function hasWorkOSConfiguration(): boolean {
  return [
    process.env.WORKOS_CLIENT_ID,
    process.env.WORKOS_COOKIE_PASSWORD,
    process.env.WORKOS_ISSUER,
    process.env.NEXT_PUBLIC_WORKOS_REDIRECT_URI,
  ].every((value) => value !== undefined && value.length > 0);
}

export default function RootLayout({ children }: RootLayoutProps) {
  const authConfigured = hasWorkOSConfiguration();
  return (
    <html className={geistSans.variable} data-buttons="solid" data-radius="soft" data-type="software" lang="en">
      <body>
        <AuthKitProvider initialAuth={authConfigured ? undefined : { user: null }} onSessionExpired={authConfigured ? undefined : false}>
          <QueryProvider>{children}</QueryProvider>
        </AuthKitProvider>
      </body>
    </html>
  );
}
