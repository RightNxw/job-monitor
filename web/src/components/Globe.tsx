"use client";

import { useEffect, useRef, useState, useCallback } from "react";
import createGlobe from "cobe";

interface Company {
  name: string;
  domain: string;
  location: [number, number];
}

const COMPANIES: Company[] = [
  // US Tech, West Coast
  { name: "Google", domain: "google.com", location: [37.422, -122.084] },
  { name: "Apple", domain: "apple.com", location: [37.3349, -122.009] },
  { name: "Meta", domain: "meta.com", location: [37.4847, -122.1477] },
  { name: "Netflix", domain: "netflix.com", location: [37.2431, -121.979] },
  { name: "Nvidia", domain: "nvidia.com", location: [37.3713, -121.9665] },
  { name: "Salesforce", domain: "salesforce.com", location: [37.7935, -122.3963] },
  { name: "Adobe", domain: "adobe.com", location: [37.3319, -121.8934] },
  { name: "Uber", domain: "uber.com", location: [37.7752, -122.4176] },
  { name: "Airbnb", domain: "airbnb.com", location: [37.7726, -122.4197] },
  { name: "Stripe", domain: "stripe.com", location: [37.7749, -122.4194] },
  { name: "Coinbase", domain: "coinbase.com", location: [37.9101, -122.0652] },
  // US Tech, Other
  { name: "Amazon", domain: "amazon.com", location: [47.6062, -122.3321] },
  { name: "Microsoft", domain: "microsoft.com", location: [47.6423, -122.1391] },
  { name: "Tesla", domain: "tesla.com", location: [30.2218, -97.6472] },
  // US Finance
  { name: "JPMorgan", domain: "jpmorganchase.com", location: [40.7527, -73.9772] },
  { name: "Goldman Sachs", domain: "goldmansachs.com", location: [40.7143, -74.0133] },
  { name: "Morgan Stanley", domain: "morganstanley.com", location: [40.758, -73.9855] },
  { name: "Citadel", domain: "citadel.com", location: [41.8827, -87.6233] },
  { name: "Jane Street", domain: "janestreet.com", location: [40.7408, -74.0074] },
  // US Consulting
  { name: "Deloitte", domain: "deloitte.com", location: [40.7128, -74.006] },
  { name: "McKinsey", domain: "mckinsey.com", location: [40.758, -73.9855] },
  { name: "BCG", domain: "bcg.com", location: [42.3601, -71.0589] },
  { name: "Bain", domain: "bain.com", location: [42.3554, -71.064] },
  // Europe
  { name: "Spotify", domain: "spotify.com", location: [59.3336, 18.0565] },
  { name: "Klarna", domain: "klarna.com", location: [59.3346, 18.0632] },
  { name: "Revolut", domain: "revolut.com", location: [51.5074, -0.1278] },
  { name: "HSBC", domain: "hsbc.com", location: [51.5074, -0.1278] },
  { name: "Siemens", domain: "siemens.com", location: [48.1351, 11.582] },
  { name: "SAP", domain: "sap.com", location: [49.2933, 8.6417] },
  { name: "Zalando", domain: "zalando.com", location: [52.52, 13.405] },
  { name: "Adyen", domain: "adyen.com", location: [52.3676, 4.9041] },
  { name: "LVMH", domain: "lvmh.com", location: [48.8566, 2.3522] },
  { name: "UBS", domain: "ubs.com", location: [47.3769, 8.5417] },
  { name: "Accenture", domain: "accenture.com", location: [53.3498, -6.2603] },
  // Asia
  { name: "Samsung", domain: "samsung.com", location: [37.5145, 127.1032] },
  { name: "Sony", domain: "sony.com", location: [35.6762, 139.753] },
  { name: "Toyota", domain: "toyota.com", location: [35.0564, 137.1554] },
  { name: "ByteDance", domain: "bytedance.com", location: [39.9042, 116.4074] },
  { name: "Tencent", domain: "tencent.com", location: [22.5431, 114.0579] },
  { name: "Alibaba", domain: "alibaba.com", location: [30.2741, 120.1551] },
  { name: "Infosys", domain: "infosys.com", location: [12.9716, 77.5946] },
  { name: "Grab", domain: "grab.com", location: [1.2897, 103.8501] },
  { name: "Kakao", domain: "kakao.com", location: [37.3947, 127.1112] },
  // Other
  { name: "Shopify", domain: "shopify.com", location: [45.4215, -75.6919] },
  { name: "Atlassian", domain: "atlassian.com", location: [-33.8826, 151.2077] },
  { name: "Canva", domain: "canva.com", location: [-33.8841, 151.2006] },
  { name: "Mercado Libre", domain: "mercadolibre.com", location: [-23.5505, -46.6333] },
  { name: "Rappi", domain: "rappi.com", location: [4.711, -74.0721] },
];

function latLngToScreen(
  lat: number,
  lng: number,
  phi: number,
  theta: number,
  radius: number,
  cx: number,
  cy: number
) {
  const latRad = (lat * Math.PI) / 180;
  const lngRad = (lng * Math.PI) / 180;

  const sx = Math.cos(latRad) * Math.sin(lngRad + phi);
  const sy = -Math.sin(latRad);
  const sz = Math.cos(latRad) * Math.cos(lngRad + phi);

  const cosT = Math.cos(-theta);
  const sinT = Math.sin(-theta);
  const ry = sy * cosT - sz * sinT;
  const rz = sy * sinT + sz * cosT;

  return {
    x: cx + sx * radius,
    y: cy + ry * radius,
    z: rz,
    visible: rz > 0.2,
  };
}

interface Label {
  name: string;
  domain: string;
  x: number;
  y: number;
  opacity: number;
}

export default function Globe({ className = "" }: { className?: string }) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const phiRef = useRef(0);
  const [labels, setLabels] = useState<Label[]>([]);

  const updateLabels = useCallback(() => {
    const container = containerRef.current;
    if (!container) return;

    const w = container.offsetWidth;
    const h = container.offsetHeight;
    const size = Math.min(w, h);
    const radius = size * 0.44;
    const cx = w / 2;
    const cy = h / 2;

    const newLabels: Label[] = [];
    for (const company of COMPANIES) {
      const pos = latLngToScreen(
        company.location[0],
        company.location[1],
        phiRef.current,
        0.15,
        radius,
        cx,
        cy
      );
      if (pos.visible) {
        const fade = Math.min(1, Math.max(0, (pos.z - 0.2) * 2));
        newLabels.push({
          name: company.name,
          domain: company.domain,
          x: pos.x,
          y: pos.y,
          opacity: fade * 0.9,
        });
      }
    }
    setLabels(newLabels);
  }, []);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const width = canvas.offsetWidth;

    const globe = createGlobe(canvas, {
      devicePixelRatio: 2,
      width: width * 2,
      height: width * 2,
      phi: 0,
      theta: 0.15,
      dark: 1,
      diffuse: 0.4,
      mapSamples: 40000,
      mapBrightness: 6,
      baseColor: [0.97, 0.97, 0.94],
      markerColor: [0.1, 0.1, 0.1],
      glowColor: [0.97, 0.97, 0.94],
      markers: COMPANIES.map((c) => ({
        location: c.location,
        size: 0.03,
      })),
    });

    let animationId: number;
    function animate() {
      phiRef.current += 0.003;
      globe.update({ phi: phiRef.current });
      updateLabels();
      animationId = requestAnimationFrame(animate);
    }
    animationId = requestAnimationFrame(animate);

    canvas.style.opacity = "1";

    return () => {
      cancelAnimationFrame(animationId);
      globe.destroy();
    };
  }, [updateLabels]);

  return (
    <div ref={containerRef} className={`relative ${className}`}>
      <canvas
        ref={canvasRef}
        style={{
          width: "100%",
          height: "100%",
          opacity: 0,
          transition: "opacity 1.5s ease",
          contain: "layout paint size",
          filter: "invert(1)",
        }}
      />
      {labels.map((label) => (
        <div
          key={label.name}
          className="absolute pointer-events-none"
          style={{
            left: label.x,
            top: label.y,
            transform: "translate(-50%, -50%)",
            opacity: label.opacity,
            transition: "opacity 0.1s ease",
          }}
        >
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img
            src={`https://www.google.com/s2/favicons?domain=${label.domain}&sz=64`}
            alt={label.name}
            className="w-5 h-5 sm:w-6 sm:h-6 rounded-sm object-contain drop-shadow-[0_1px_3px_rgba(0,0,0,0.3)]"
            loading="lazy"
          />
        </div>
      ))}
    </div>
  );
}
