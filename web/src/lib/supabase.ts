import { createClient } from "@supabase/supabase-js";

const url = process.env.NEXT_PUBLIC_SUPABASE_URL;
const key = process.env.NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY;

// Next prerenders the dashboard shell at build time, which evaluates this
// module. createClient throws on an empty key, so without the fallbacks below
// `npm run build` fails on a fresh clone before anyone has filled in
// .env.local. Warn loudly and let the dashboard come up empty instead.
if (!url || !key) {
  console.warn(
    "[supabase] NEXT_PUBLIC_SUPABASE_URL / NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY are unset. " +
      "The dashboard will load with no data. See web/.env.example."
  );
}

export const supabase = createClient(
  url || "https://unconfigured.supabase.co",
  key || "unconfigured"
);
