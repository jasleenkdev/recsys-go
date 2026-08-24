import { auth, signIn, signOut } from "@/auth";

export default async function Home() {
  const session = await auth();

  return (
    <main style={{ fontFamily: "var(--font-geist-sans), sans-serif", padding: "3rem" }}>
      <h1>recsys-go</h1>

      {session ? (
        <>
          <p>
            Signed in as <strong>{session.user?.name ?? session.user?.email}</strong>
          </p>
          <p>
            recsys user_id: <strong>{session.recsysUserId ?? "not synced"}</strong>
          </p>
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
    </main>
  );
}
