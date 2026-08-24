import NextAuth from "next-auth";
import GitHub from "next-auth/providers/github";

const recsysAPIURL = process.env.RECSYS_API_URL ?? "http://localhost:8081";

// syncUser maps a GitHub account onto a row in the recsys `users` table
// and returns its id. The backend upsert is idempotent, so calling this
// on every fresh sign-in reuses the same user_id rather than duplicating.
async function syncUser(externalID: string): Promise<number> {
  const res = await fetch(`${recsysAPIURL}/v1/auth/sync`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ external_id: externalID }),
  });

  if (!res.ok) {
    throw new Error(`auth sync returned ${res.status}`);
  }

  const body = (await res.json()) as { user_id: number };
  return body.user_id;
}

export const { handlers, auth, signIn, signOut } = NextAuth({
  providers: [GitHub],
  callbacks: {
    // `account` is only set on a fresh sign-in, so the sync runs once per
    // sign-in rather than on every token refresh. A failure here throws,
    // which fails the sign-in — better than admitting a user we have no
    // recsys user_id for, since every downstream endpoint needs one.
    async jwt({ token, account, profile }) {
      if (account && profile) {
        // GitHub's numeric id, not the login: usernames can be changed,
        // the id cannot.
        token.recsysUserId = await syncUser(`github:${profile.id}`);
      }
      return token;
    },
    async session({ session, token }) {
      session.recsysUserId = token.recsysUserId;
      return session;
    },
  },
});
