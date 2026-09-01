import { SearchPanel } from "@/app/components/SearchPanel";

export default function SearchPage() {
  return (
    <main className="container">
      <h1>Grounded search</h1>
      <p className="lede">
        Natural-language search over repository READMEs. Answers are written
        only from retrieved chunks — if nothing is relevant enough, you get
        told that instead of a guess.
      </p>
      <SearchPanel />
    </main>
  );
}
