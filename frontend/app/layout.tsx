import type { Metadata } from "next";
import Link from "next/link";
import { Geist, Geist_Mono } from "next/font/google";
import { auth, signIn, signOut } from "@/auth";
import "./globals.css";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "recsys-go",
  description: "GitHub repository recommendations and grounded README search",
};

export default async function RootLayout({ children }: LayoutProps<"/">) {
  const session = await auth();

  return (
    <html lang="en" className={`${geistSans.variable} ${geistMono.variable}`}>
      <body>
        <header className="topbar">
          <Link href="/" className="brand">
            recsys-go
          </Link>
          <nav className="nav">
            <Link href="/">Browse</Link>
            <Link href="/recommendations">For you</Link>
            <Link href="/search">Search</Link>
          </nav>
          <div className="spacer" />
          <div className="who">
            {session ? (
              <>
                <span>
                  {session.user?.name ?? session.user?.email} · user_id{" "}
                  <strong>{session.recsysUserId ?? "not synced"}</strong>
                </span>
                <form
                  action={async () => {
                    "use server";
                    await signOut();
                  }}
                >
                  <button type="submit">Sign out</button>
                </form>
              </>
            ) : (
              <form
                action={async () => {
                  "use server";
                  await signIn("github");
                }}
              >
                <button type="submit">Sign in with GitHub</button>
              </form>
            )}
          </div>
        </header>
        {children}
      </body>
    </html>
  );
}
