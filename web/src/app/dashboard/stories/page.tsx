"use client";

import { useEffect, useState } from "react";
import { supabase } from "@/lib/supabase";

interface Story {
  id: number;
  title: string;
  url: string;
  metadata: {
    type?: string;
    links?: string[];
    media_url?: string;
    image?: string;
  };
  posted_at: string;
  discovered_at: string;
}

export default function Stories() {
  const [stories, setStories] = useState<Story[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function fetch() {
      const { data, error } = await supabase
        .from("jobs")
        .select("*")
        .eq("source", "instagram:zero2sudo")
        .order("posted_at", { ascending: false })
        .limit(50);

      if (!error && data) {
        setStories(
          data.map((d: Record<string, unknown>) => ({
            ...d,
            metadata: typeof d.metadata === "string" ? JSON.parse(d.metadata as string) : d.metadata || {},
          })) as Story[]
        );
      }
      setLoading(false);
    }
    fetch();
  }, []);

  return (
    <div>
      <div className="mb-8">
        <h1 className="text-2xl font-bold mb-1">@zero2sudo Stories</h1>
        <p className="text-white/40 text-sm">
          Instagram stories from zero2sudo with OCR-extracted text and application links.
        </p>
      </div>

      {loading ? (
        <div className="text-white/30 text-center py-20">Loading...</div>
      ) : stories.length === 0 ? (
        <div className="text-white/30 text-center py-20">
          No stories captured yet. The Instagram monitor needs to run first.
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {stories.map((story) => (
            <a
              key={story.id}
              href={story.url}
              target="_blank"
              rel="noopener noreferrer"
              className="bg-white/[0.02] border border-white/[0.06] rounded-lg overflow-hidden hover:bg-white/[0.04] hover:border-white/[0.1] transition-all group"
            >
              {/* Thumbnail */}
              {(story.metadata.image || story.metadata.media_url) && (
                <div className="aspect-[9/16] max-h-64 overflow-hidden bg-black/50">
                  {/* eslint-disable-next-line @next/next/no-img-element */}
                  <img
                    src={story.metadata.image || story.metadata.media_url}
                    alt=""
                    className="w-full h-full object-cover opacity-80 group-hover:opacity-100 transition-opacity"
                  />
                </div>
              )}

              <div className="px-4 py-3">
                <p className="text-sm text-white/70 line-clamp-3 leading-relaxed">
                  {story.title}
                </p>

                {/* Extracted links */}
                {story.metadata.links && story.metadata.links.length > 0 && (
                  <div className="mt-2 space-y-1">
                    {story.metadata.links.map((link, i) => (
                      <div
                        key={i}
                        className="text-[11px] text-blue-400/70 truncate"
                      >
                        {link}
                      </div>
                    ))}
                  </div>
                )}

                <div className="flex items-center justify-between mt-2 text-[11px] text-white/25">
                  <span>{story.metadata.type || "story"}</span>
                  <span>{new Date(story.posted_at).toLocaleDateString()}</span>
                </div>
              </div>
            </a>
          ))}
        </div>
      )}
    </div>
  );
}
