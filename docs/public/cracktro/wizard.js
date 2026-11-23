// Cracktro workflow wizard (W): the gh-aw wizard's flow (why, how, when, where, extras, agent, prompt) told as a
// choose-your-own-adventure gamebook. Data and prompt generator come from wizard-data.js (window.CracktroWizardData);
// runtime.js adds the dock button and the W key when create() receives { wizard: window.CracktroWizard }.
(function () {
  "use strict";

  // Octicons (MIT, primer/octicons), 16px paths, for the wizard's option icons.
  const OCTI = {
    "alert": "M6.457 1.047c.659-1.234 2.427-1.234 3.086 0l6.082 11.378A1.75 1.75 0 0 1 14.082 15H1.918a1.75 1.75 0 0 1-1.543-2.575Zm1.763.707a.25.25 0 0 0-.44 0L1.698 13.132a.25.25 0 0 0 .22.368h12.164a.25.25 0 0 0 .22-.368Zm.53 3.996v2.5a.75.75 0 0 1-1.5 0v-2.5a.75.75 0 0 1 1.5 0ZM9 11a1 1 0 1 1-2 0 1 1 0 0 1 2 0Z",
    "arrow-left": "M7.78 12.53a.75.75 0 0 1-1.06 0L2.47 8.28a.75.75 0 0 1 0-1.06l4.25-4.25a.751.751 0 0 1 1.042.018.751.751 0 0 1 .018 1.042L4.81 7h7.44a.75.75 0 0 1 0 1.5H4.81l2.97 2.97a.75.75 0 0 1 0 1.06Z",
    "arrow-right": "M8.22 2.97a.75.75 0 0 1 1.06 0l4.25 4.25a.75.75 0 0 1 0 1.06l-4.25 4.25a.751.751 0 0 1-1.042-.018.751.751 0 0 1-.018-1.042l2.97-2.97H3.75a.75.75 0 0 1 0-1.5h7.44L8.22 4.03a.75.75 0 0 1 0-1.06Z",
    "book": "M0 1.75A.75.75 0 0 1 .75 1h4.253c1.227 0 2.317.59 3 1.501A3.743 3.743 0 0 1 11.006 1h4.245a.75.75 0 0 1 .75.75v10.5a.75.75 0 0 1-.75.75h-4.507a2.25 2.25 0 0 0-1.591.659l-.622.621a.75.75 0 0 1-1.06 0l-.622-.621A2.25 2.25 0 0 0 5.258 13H.75a.75.75 0 0 1-.75-.75Zm7.251 10.324.004-5.073-.002-2.253A2.25 2.25 0 0 0 5.003 2.5H1.5v9h3.757a3.75 3.75 0 0 1 1.994.574ZM8.755 4.75l-.004 7.322a3.752 3.752 0 0 1 1.992-.572H14.5v-9h-3.495a2.25 2.25 0 0 0-2.25 2.25Z",
    "cache": "M2.5 5.724V8c0 .248.238.7 1.169 1.159.874.43 2.144.745 3.62.822a.75.75 0 1 1-.078 1.498c-1.622-.085-3.102-.432-4.204-.975a5.565 5.565 0 0 1-.507-.28V12.5c0 .133.058.318.282.551.227.237.591.483 1.101.707 1.015.447 2.47.742 4.117.742.406 0 .802-.018 1.183-.052a.751.751 0 1 1 .134 1.494C8.89 15.98 8.45 16 8 16c-1.805 0-3.475-.32-4.721-.869-.623-.274-1.173-.619-1.579-1.041-.408-.425-.7-.964-.7-1.59v-9c0-.626.292-1.165.7-1.591.406-.42.956-.766 1.579-1.04C4.525.32 6.195 0 8 0c1.806 0 3.476.32 4.721.869.623.274 1.173.619 1.579 1.041.408.425.7.964.7 1.59 0 .626-.292 1.165-.7 1.591-.406.42-.956.766-1.578 1.04C11.475 6.68 9.805 7 8 7c-1.805 0-3.475-.32-4.721-.869a6.15 6.15 0 0 1-.779-.407Zm0-2.224c0 .133.058.318.282.551.227.237.591.483 1.101.707C4.898 5.205 6.353 5.5 8 5.5c1.646 0 3.101-.295 4.118-.742.508-.224.873-.471 1.1-.708.224-.232.282-.417.282-.55 0-.133-.058-.318-.282-.551-.227-.237-.591-.483-1.101-.707C11.102 1.795 9.647 1.5 8 1.5c-1.646 0-3.101.295-4.118.742-.508.224-.873.471-1.1.708-.224.232-.282.417-.282.55Z M14.49 7.582a.375.375 0 0 0-.66-.313l-3.625 4.625a.375.375 0 0 0 .295.606h2.127l-.619 2.922a.375.375 0 0 0 .666.304l3.125-4.125A.375.375 0 0 0 15.5 11h-1.778l.769-3.418Z",
    "calendar": "M4.75 0a.75.75 0 0 1 .75.75V2h5V.75a.75.75 0 0 1 1.5 0V2h1.25c.966 0 1.75.784 1.75 1.75v10.5A1.75 1.75 0 0 1 13.25 16H2.75A1.75 1.75 0 0 1 1 14.25V3.75C1 2.784 1.784 2 2.75 2H4V.75A.75.75 0 0 1 4.75 0ZM2.5 7.5v6.75c0 .138.112.25.25.25h10.5a.25.25 0 0 0 .25-.25V7.5Zm10.75-4H2.75a.25.25 0 0 0-.25.25V6h11V3.75a.25.25 0 0 0-.25-.25Z",
    "check": "M13.78 4.22a.75.75 0 0 1 0 1.06l-7.25 7.25a.75.75 0 0 1-1.06 0L2.22 9.28a.751.751 0 0 1 .018-1.042.751.751 0 0 1 1.042-.018L6 10.94l6.72-6.72a.75.75 0 0 1 1.06 0Z",
    "checklist": "M2.5 1.75v11.5c0 .138.112.25.25.25h3.17a.75.75 0 0 1 0 1.5H2.75A1.75 1.75 0 0 1 1 13.25V1.75C1 .784 1.784 0 2.75 0h8.5C12.216 0 13 .784 13 1.75v7.736a.75.75 0 0 1-1.5 0V1.75a.25.25 0 0 0-.25-.25h-8.5a.25.25 0 0 0-.25.25Zm13.274 9.537v-.001l-4.557 4.45a.75.75 0 0 1-1.055-.008l-1.943-1.95a.75.75 0 0 1 1.062-1.058l1.419 1.425 4.026-3.932a.75.75 0 1 1 1.048 1.074ZM4.75 4h4.5a.75.75 0 0 1 0 1.5h-4.5a.75.75 0 0 1 0-1.5ZM4 7.75A.75.75 0 0 1 4.75 7h2a.75.75 0 0 1 0 1.5h-2A.75.75 0 0 1 4 7.75Z",
    "comment-discussion": "M1.75 1h8.5c.966 0 1.75.784 1.75 1.75v5.5A1.75 1.75 0 0 1 10.25 10H7.061l-2.574 2.573A1.458 1.458 0 0 1 2 11.543V10h-.25A1.75 1.75 0 0 1 0 8.25v-5.5C0 1.784.784 1 1.75 1ZM1.5 2.75v5.5c0 .138.112.25.25.25h1a.75.75 0 0 1 .75.75v2.19l2.72-2.72a.749.749 0 0 1 .53-.22h3.5a.25.25 0 0 0 .25-.25v-5.5a.25.25 0 0 0-.25-.25h-8.5a.25.25 0 0 0-.25.25Zm13 2a.25.25 0 0 0-.25-.25h-.5a.75.75 0 0 1 0-1.5h.5c.966 0 1.75.784 1.75 1.75v5.5A1.75 1.75 0 0 1 14.25 12H14v1.543a1.458 1.458 0 0 1-2.487 1.03L9.22 12.28a.749.749 0 0 1 .326-1.275.749.749 0 0 1 .734.215l2.22 2.22v-2.19a.75.75 0 0 1 .75-.75h1a.25.25 0 0 0 .25-.25Z",
    "copy": "M0 6.75C0 5.784.784 5 1.75 5h1.5a.75.75 0 0 1 0 1.5h-1.5a.25.25 0 0 0-.25.25v7.5c0 .138.112.25.25.25h7.5a.25.25 0 0 0 .25-.25v-1.5a.75.75 0 0 1 1.5 0v1.5A1.75 1.75 0 0 1 9.25 16h-7.5A1.75 1.75 0 0 1 0 14.25Z M5 1.75C5 .784 5.784 0 6.75 0h7.5C15.216 0 16 .784 16 1.75v7.5A1.75 1.75 0 0 1 14.25 11h-7.5A1.75 1.75 0 0 1 5 9.25Zm1.75-.25a.25.25 0 0 0-.25.25v7.5c0 .138.112.25.25.25h7.5a.25.25 0 0 0 .25-.25v-7.5a.25.25 0 0 0-.25-.25Z",
    "dependabot": "M5.75 7.5a.75.75 0 0 1 .75.75v1.5a.75.75 0 0 1-1.5 0v-1.5a.75.75 0 0 1 .75-.75Zm5.25.75a.75.75 0 0 0-1.5 0v1.5a.75.75 0 0 0 1.5 0v-1.5Z M6.25 0h2A.75.75 0 0 1 9 .75V3.5h3.25a2.25 2.25 0 0 1 2.25 2.25V8h.75a.75.75 0 0 1 0 1.5h-.75v2.75a2.25 2.25 0 0 1-2.25 2.25h-8.5a2.25 2.25 0 0 1-2.25-2.25V9.5H.75a.75.75 0 0 1 0-1.5h.75V5.75A2.25 2.25 0 0 1 3.75 3.5H7.5v-2H6.25a.75.75 0 0 1 0-1.5ZM3 5.75v6.5c0 .414.336.75.75.75h8.5a.75.75 0 0 0 .75-.75v-6.5a.75.75 0 0 0-.75-.75h-8.5a.75.75 0 0 0-.75.75Z",
    "device-desktop": "M14.25 1c.966 0 1.75.784 1.75 1.75v7.5A1.75 1.75 0 0 1 14.25 12h-3.727c.099 1.041.52 1.872 1.292 2.757A.752.752 0 0 1 11.25 16h-6.5a.75.75 0 0 1-.565-1.243c.772-.885 1.192-1.716 1.292-2.757H1.75A1.75 1.75 0 0 1 0 10.25v-7.5C0 1.784.784 1 1.75 1ZM1.75 2.5a.25.25 0 0 0-.25.25v7.5c0 .138.112.25.25.25h12.5a.25.25 0 0 0 .25-.25v-7.5a.25.25 0 0 0-.25-.25ZM9.018 12H6.982a5.72 5.72 0 0 1-.765 2.5h3.566a5.72 5.72 0 0 1-.765-2.5Z",
    "eye": "M8 2c1.981 0 3.671.992 4.933 2.078 1.27 1.091 2.187 2.345 2.637 3.023a1.62 1.62 0 0 1 0 1.798c-.45.678-1.367 1.932-2.637 3.023C11.67 13.008 9.981 14 8 14c-1.981 0-3.671-.992-4.933-2.078C1.797 10.83.88 9.576.43 8.898a1.62 1.62 0 0 1 0-1.798c.45-.677 1.367-1.931 2.637-3.022C4.33 2.992 6.019 2 8 2ZM1.679 7.932a.12.12 0 0 0 0 .136c.411.622 1.241 1.75 2.366 2.717C5.176 11.758 6.527 12.5 8 12.5c1.473 0 2.825-.742 3.955-1.715 1.124-.967 1.954-2.096 2.366-2.717a.12.12 0 0 0 0-.136c-.412-.621-1.242-1.75-2.366-2.717C10.824 4.242 9.473 3.5 8 3.5c-1.473 0-2.825.742-3.955 1.715-1.124.967-1.954 2.096-2.366 2.717ZM8 10a2 2 0 1 1-.001-3.999A2 2 0 0 1 8 10Z",
    "git-commit": "M11.93 8.5a4.002 4.002 0 0 1-7.86 0H.75a.75.75 0 0 1 0-1.5h3.32a4.002 4.002 0 0 1 7.86 0h3.32a.75.75 0 0 1 0 1.5Zm-1.43-.75a2.5 2.5 0 1 0-5 0 2.5 2.5 0 0 0 5 0Z",
    "git-pull-request": "M1.5 3.25a2.25 2.25 0 1 1 3 2.122v5.256a2.251 2.251 0 1 1-1.5 0V5.372A2.25 2.25 0 0 1 1.5 3.25Zm5.677-.177L9.573.677A.25.25 0 0 1 10 .854V2.5h1A2.5 2.5 0 0 1 13.5 5v5.628a2.251 2.251 0 1 1-1.5 0V5a1 1 0 0 0-1-1h-1v1.646a.25.25 0 0 1-.427.177L7.177 3.427a.25.25 0 0 1 0-.354ZM3.75 2.5a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Zm0 9.5a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Zm8.25.75a.75.75 0 1 0 1.5 0 .75.75 0 0 0-1.5 0Z",
    "graph": "M1.5 1.75V13.5h13.75a.75.75 0 0 1 0 1.5H.75a.75.75 0 0 1-.75-.75V1.75a.75.75 0 0 1 1.5 0Zm14.28 2.53-5.25 5.25a.75.75 0 0 1-1.06 0L7 7.06 4.28 9.78a.751.751 0 0 1-1.042-.018.751.751 0 0 1-.018-1.042l3.25-3.25a.75.75 0 0 1 1.06 0L10 7.94l4.72-4.72a.751.751 0 0 1 1.042.018.751.751 0 0 1 .018 1.042Z",
    "issue-opened": "M8 9.5a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3Z M8 0a8 8 0 1 1 0 16A8 8 0 0 1 8 0ZM1.5 8a6.5 6.5 0 1 0 13 0 6.5 6.5 0 0 0-13 0Z",
    "link": "m7.775 3.275 1.25-1.25a3.5 3.5 0 1 1 4.95 4.95l-2.5 2.5a3.5 3.5 0 0 1-4.95 0 .751.751 0 0 1 .018-1.042.751.751 0 0 1 1.042-.018 1.998 1.998 0 0 0 2.83 0l2.5-2.5a2.002 2.002 0 0 0-2.83-2.83l-1.25 1.25a.751.751 0 0 1-1.042-.018.751.751 0 0 1-.018-1.042Zm-4.69 9.64a1.998 1.998 0 0 0 2.83 0l1.25-1.25a.751.751 0 0 1 1.042.018.751.751 0 0 1 .018 1.042l-1.25 1.25a3.5 3.5 0 1 1-4.95-4.95l2.5-2.5a3.5 3.5 0 0 1 4.95 0 .751.751 0 0 1-.018 1.042.751.751 0 0 1-1.042.018 1.998 1.998 0 0 0-2.83 0l-2.5 2.5a1.998 1.998 0 0 0 0 2.83Z",
    "milestone": "M7.75 0a.75.75 0 0 1 .75.75V3h3.634c.414 0 .814.147 1.13.414l2.07 1.75a1.75 1.75 0 0 1 0 2.672l-2.07 1.75a1.75 1.75 0 0 1-1.13.414H8.5v5.25a.75.75 0 0 1-1.5 0V10H2.75A1.75 1.75 0 0 1 1 8.25v-3.5C1 3.784 1.784 3 2.75 3H7V.75A.75.75 0 0 1 7.75 0Zm4.384 8.5a.25.25 0 0 0 .161-.06l2.07-1.75a.248.248 0 0 0 0-.38l-2.07-1.75a.25.25 0 0 0-.161-.06H2.75a.25.25 0 0 0-.25.25v3.5c0 .138.112.25.25.25h9.384Z",
    "package": "m8.878.392 5.25 3.045c.54.314.872.89.872 1.514v6.098a1.75 1.75 0 0 1-.872 1.514l-5.25 3.045a1.75 1.75 0 0 1-1.756 0l-5.25-3.045A1.75 1.75 0 0 1 1 11.049V4.951c0-.624.332-1.201.872-1.514L7.122.392a1.75 1.75 0 0 1 1.756 0ZM7.875 1.69l-4.63 2.685L8 7.133l4.755-2.758-4.63-2.685a.248.248 0 0 0-.25 0ZM2.5 5.677v5.372c0 .09.047.171.125.216l4.625 2.683V8.432Zm6.25 8.271 4.625-2.683a.25.25 0 0 0 .125-.216V5.677L8.75 8.432Z",
    "pencil": "M11.013 1.427a1.75 1.75 0 0 1 2.474 0l1.086 1.086a1.75 1.75 0 0 1 0 2.474l-8.61 8.61c-.21.21-.47.364-.756.445l-3.251.93a.75.75 0 0 1-.927-.928l.929-3.25c.081-.286.235-.547.445-.758l8.61-8.61Zm.176 4.823L9.75 4.81l-6.286 6.287a.253.253 0 0 0-.064.108l-.558 1.953 1.953-.558a.253.253 0 0 0 .108-.064Zm1.238-3.763a.25.25 0 0 0-.354 0L10.811 3.75l1.439 1.44 1.263-1.263a.25.25 0 0 0 0-.354Z",
    "plug": "M4 8H2.5a1 1 0 0 0-1 1v5.25a.75.75 0 0 1-1.5 0V9a2.5 2.5 0 0 1 2.5-2.5H4V5.133a1.75 1.75 0 0 1 1.533-1.737l2.831-.353.76-.913c.332-.4.825-.63 1.344-.63h.782c.966 0 1.75.784 1.75 1.75V4h2.25a.75.75 0 0 1 0 1.5H13v4h2.25a.75.75 0 0 1 0 1.5H13v.75a1.75 1.75 0 0 1-1.75 1.75h-.782c-.519 0-1.012-.23-1.344-.63l-.761-.912-2.83-.354A1.75 1.75 0 0 1 4 9.867Zm6.276-4.91-.95 1.14a.753.753 0 0 1-.483.265l-3.124.39a.25.25 0 0 0-.219.248v4.734c0 .126.094.233.219.249l3.124.39a.752.752 0 0 1 .483.264l.95 1.14a.25.25 0 0 0 .192.09h.782a.25.25 0 0 0 .25-.25v-8.5a.25.25 0 0 0-.25-.25h-.782a.25.25 0 0 0-.192.09Z",
    "repo": "M2 2.5A2.5 2.5 0 0 1 4.5 0h8.75a.75.75 0 0 1 .75.75v12.5a.75.75 0 0 1-.75.75h-2.5a.75.75 0 0 1 0-1.5h1.75v-2h-8a1 1 0 0 0-.714 1.7.75.75 0 1 1-1.072 1.05A2.495 2.495 0 0 1 2 11.5Zm10.5-1h-8a1 1 0 0 0-1 1v6.708A2.486 2.486 0 0 1 4.5 9h8ZM5 12.25a.25.25 0 0 1 .25-.25h3.5a.25.25 0 0 1 .25.25v3.25a.25.25 0 0 1-.4.2l-1.45-1.087a.249.249 0 0 0-.3 0L5.4 15.7a.25.25 0 0 1-.4-.2Z",
    "shield": "M7.467.133a1.748 1.748 0 0 1 1.066 0l5.25 1.68A1.75 1.75 0 0 1 15 3.48V7c0 1.566-.32 3.182-1.303 4.682-.983 1.498-2.585 2.813-5.032 3.855a1.697 1.697 0 0 1-1.33 0c-2.447-1.042-4.049-2.357-5.032-3.855C1.32 10.182 1 8.566 1 7V3.48a1.75 1.75 0 0 1 1.217-1.667Zm.61 1.429a.25.25 0 0 0-.153 0l-5.25 1.68a.25.25 0 0 0-.174.238V7c0 1.358.275 2.666 1.057 3.86.784 1.194 2.121 2.34 4.366 3.297a.196.196 0 0 0 .154 0c2.245-.956 3.582-2.104 4.366-3.298C13.225 9.666 13.5 8.36 13.5 7V3.48a.251.251 0 0 0-.174-.237l-5.25-1.68ZM8.75 4.75v3a.75.75 0 0 1-1.5 0v-3a.75.75 0 0 1 1.5 0ZM9 10.5a1 1 0 1 1-2 0 1 1 0 0 1 2 0Z",
    "sparkle-fill": "M7.53 1.282a.5.5 0 0 1 .94 0l.478 1.306a7.492 7.492 0 0 0 4.464 4.464l1.305.478a.5.5 0 0 1 0 .94l-1.305.478a7.492 7.492 0 0 0-4.464 4.464l-.478 1.305a.5.5 0 0 1-.94 0l-.478-1.305a7.492 7.492 0 0 0-4.464-4.464L1.282 8.47a.5.5 0 0 1 0-.94l1.306-.478a7.492 7.492 0 0 0 4.464-4.464Z",
    "tag": "M1 7.775V2.75C1 1.784 1.784 1 2.75 1h5.025c.464 0 .91.184 1.238.513l6.25 6.25a1.75 1.75 0 0 1 0 2.474l-5.026 5.026a1.75 1.75 0 0 1-2.474 0l-6.25-6.25A1.752 1.752 0 0 1 1 7.775Zm1.5 0c0 .066.026.13.073.177l6.25 6.25a.25.25 0 0 0 .354 0l5.025-5.025a.25.25 0 0 0 0-.354l-6.25-6.25a.25.25 0 0 0-.177-.073H2.75a.25.25 0 0 0-.25.25ZM6 5a1 1 0 1 1 0 2 1 1 0 0 1 0-2Z",
    "terminal": "M0 2.75C0 1.784.784 1 1.75 1h12.5c.966 0 1.75.784 1.75 1.75v10.5A1.75 1.75 0 0 1 14.25 15H1.75A1.75 1.75 0 0 1 0 13.25Zm1.75-.25a.25.25 0 0 0-.25.25v10.5c0 .138.112.25.25.25h12.5a.25.25 0 0 0 .25-.25V2.75a.25.25 0 0 0-.25-.25ZM7.25 8a.749.749 0 0 1-.22.53l-2.25 2.25a.749.749 0 0 1-1.275-.326.749.749 0 0 1 .215-.734L5.44 8 3.72 6.28a.749.749 0 0 1 .326-1.275.749.749 0 0 1 .734.215l2.25 2.25c.141.14.22.331.22.53Zm1.5 1.5h3a.75.75 0 0 1 0 1.5h-3a.75.75 0 0 1 0-1.5Z",
    "tools": "M5.433 2.304A4.492 4.492 0 0 0 3.5 6c0 1.598.832 3.002 2.09 3.802.518.328.929.923.902 1.64v.008l-.164 3.337a.75.75 0 1 1-1.498-.073l.163-3.33c.002-.085-.05-.216-.207-.316A5.996 5.996 0 0 1 2 6a5.993 5.993 0 0 1 2.567-4.92 1.482 1.482 0 0 1 1.673-.04c.462.296.76.827.76 1.423v2.82c0 .082.041.16.11.206l.75.51a.25.25 0 0 0 .28 0l.75-.51A.249.249 0 0 0 9 5.282V2.463c0-.596.298-1.127.76-1.423a1.482 1.482 0 0 1 1.673.04A5.993 5.993 0 0 1 14 6a5.996 5.996 0 0 1-2.786 5.068c-.157.1-.209.23-.207.315l.163 3.33a.752.752 0 0 1-1.094.714.75.75 0 0 1-.404-.64l-.164-3.345c-.027-.717.384-1.312.902-1.64A4.495 4.495 0 0 0 12.5 6a4.492 4.492 0 0 0-1.933-3.696c-.024.017-.067.067-.067.16v2.818a1.75 1.75 0 0 1-.767 1.448l-.75.51a1.75 1.75 0 0 1-1.966 0l-.75-.51A1.75 1.75 0 0 1 5.5 5.282V2.463c0-.092-.043-.142-.067-.159Z",
    "x": "M3.72 3.72a.75.75 0 0 1 1.06 0L8 6.94l3.22-3.22a.749.749 0 0 1 1.275.326.749.749 0 0 1-.215.734L9.06 8l3.22 3.22a.749.749 0 0 1-.326 1.275.749.749 0 0 1-.734-.215L8 9.06l-3.22 3.22a.751.751 0 0 1-1.042-.018.751.751 0 0 1-.018-1.042L6.94 8 3.72 4.78a.75.75 0 0 1 0-1.06Z",
    "zap": "M9.504.43a1.516 1.516 0 0 1 2.437 1.713L10.415 5.5h2.123c1.57 0 2.346 1.909 1.22 3.004l-7.34 7.142a1.249 1.249 0 0 1-.871.354h-.302a1.25 1.25 0 0 1-1.157-1.723L5.633 10.5H3.462c-1.57 0-2.346-1.909-1.22-3.004L9.503.429Zm1.047 1.074L3.286 8.571A.25.25 0 0 0 3.462 9H6.75a.75.75 0 0 1 .694 1.034l-1.713 4.188 6.982-6.793A.25.25 0 0 0 12.538 7H9.25a.75.75 0 0 1-.683-1.06l2.008-4.418.003-.006a.036.036 0 0 0-.004-.009l-.006-.006-.008-.001c-.003 0-.006.002-.009.004Z",
  };

  const STYLE = `
  .wz { position: fixed; inset: 0; width: 100%; height: 100%; max-width: none; max-height: none; margin: 0; padding: 0; border: 0; background: #0d1117; color: #f0f6fc;
    font: 400 17px/1.5 "Mona Sans", -apple-system, "Segoe UI", sans-serif; font-variation-settings: "wdth" 100; overflow: hidden; --gold: #e3b341; --gold-d: #d29922; --mono: "Monaspace Neon", ui-monospace, SFMono-Regular, Menlo, monospace; }
  .wz[open] { display: block; animation: wz-open .5s cubic-bezier(.2, .8, .2, 1); }
  .wz::backdrop { background: #010409; }
  @keyframes wz-open { from { opacity: 0; transform: scale(1.03); } }
  .wz button { font: inherit; color: inherit; }
  .wz :focus-visible { outline: 2px solid var(--gold); outline-offset: 2px; }
  .wz svg.oc { width: 16px; height: 16px; fill: currentColor; flex: none; }
  /* drifting dust and a faint survey grid */
  .wz-sky { position: absolute; inset: 0; pointer-events: none; background-image: radial-gradient(#3d444d55 1px, transparent 1.5px); background-size: 28px 28px;
    mask-image: radial-gradient(ellipse at 50% 40%, #000 30%, transparent 85%); }
  .wz-dust { position: absolute; width: 3px; height: 3px; background: var(--gold); opacity: 0; animation: wz-dust var(--t) linear var(--d) infinite; }
  @keyframes wz-dust { 0% { opacity: 0; transform: translate(0, 0); } 15%, 70% { opacity: .5; } 100% { opacity: 0; transform: translate(var(--dx), -220px); } }
  .wz-top { position: absolute; top: 0; left: 0; right: 0; height: 56px; display: flex; align-items: center; gap: 14px; padding: 0 24px; z-index: 3; }
  .wz-brand { font: 600 12px/1 var(--mono); letter-spacing: .14em; text-transform: uppercase; color: #9198a1; display: flex; gap: 10px; align-items: center; }
  .wz-brand b { color: var(--gold); font-weight: 600; }
  .wz-x { margin-left: auto; width: 34px; height: 34px; border: 1px solid #3d444d; border-radius: 8px; background: #151b23cc; color: #9198a1; cursor: pointer; display: grid; place-items: center; }
  .wz-x:hover { color: #f0f6fc; border-color: #9198a1; }
  .wz-body { position: absolute; inset: 56px 0 0; display: grid; grid-template-columns: 300px minmax(0, 1fr); gap: 32px; padding: 8px 40px 32px; max-width: 1340px; margin: 0 auto; }
  .wz.cover .wz-body { grid-template-columns: minmax(0, 1fr); }
  .wz.cover .wz-side { display: none; }
  /* --- the trail map --- */
  .wz-side { display: flex; flex-direction: column; gap: 18px; min-height: 0; }
  .wz-map { position: relative; width: 300px; height: 420px; flex: none; }
  .wz-map svg.trail { position: absolute; inset: 0; overflow: visible; }
  .wz-seg { fill: none; stroke: #3d444d; stroke-width: 2; stroke-dasharray: 2 7; stroke-linecap: round; }
  .wz-lit { fill: none; stroke: var(--gold); stroke-width: 3; stroke-linecap: round; stroke-dasharray: 1; stroke-dashoffset: 1; transition: stroke-dashoffset .7s ease-in-out; }
  .wz-lit.on { stroke-dashoffset: 0; }
  .wz-me { fill: #f0f6fc; filter: drop-shadow(0 0 6px #e3b34155); }
  .wz-me-ring { fill: none; stroke: var(--gold); stroke-width: 2; transform-box: fill-box; transform-origin: center; animation: wz-ring 1.8s ease-out infinite; }
  @keyframes wz-ring { from { opacity: .9; transform: scale(.6); } to { opacity: 0; transform: scale(1.9); } }
  .wz-node { position: absolute; transform: translate(-13px, -13px); display: flex; align-items: center; gap: 10px; padding: 0; border: 0; background: none; cursor: pointer; white-space: nowrap; }
  .wz-node i { width: 26px; height: 26px; border-radius: 50%; border: 2px solid #3d444d; background: #0d1117; display: grid; place-items: center; font: 700 11px/1 var(--mono); font-style: normal; color: #9198a1;
    transition: background .3s, border-color .3s, color .3s; }
  .wz-node span { font: 600 12px/1 var(--mono); letter-spacing: .1em; text-transform: uppercase; color: #9198a1; transition: color .3s; }
  .wz-node.left { flex-direction: row-reverse; transform: translate(calc(-100% + 13px), -13px); }
  .wz-node.seen i { border-color: var(--gold); color: var(--gold); }
  .wz-node.done i { background: var(--gold); border-color: var(--gold); color: #0d1117; }
  .wz-node.cur span { color: #f0f6fc; }
  .wz-node.cur i { box-shadow: 0 0 0 4px #e3b34133; }
  .wz-node:disabled { cursor: not-allowed; }
  .wz-node:disabled i { border-style: dashed; }
  .wz-node:not(:disabled):hover span { color: #f0f6fc; }
  /* --- the tale so far --- */
  .wz-tale { border-top: 1px solid #3d444d; padding-top: 14px; overflow-y: auto; min-height: 0; }
  .wz-tale h3 { margin: 0 0 8px; font: 600 12px/1 var(--mono); letter-spacing: .14em; text-transform: uppercase; color: #9198a1; }
  .wz-tale p { margin: 0; font-size: 15px; line-height: 1.6; color: #c9d1d9; }
  .wz-tale .todo { color: #9198a1; border-bottom: 1px dashed #3d444d; }
  .wz-tale .ink { color: #f0f6fc; font-weight: 600; animation: wz-ink .8s ease-out; }
  @keyframes wz-ink { from { color: var(--gold); text-shadow: 0 0 12px #e3b34155; } }
  /* --- the book: pages flip on a left spine --- */
  .wz-book { position: relative; min-height: 0; perspective: 2400px; }
  .wz-page { position: absolute; inset: 0; display: flex; flex-direction: column; background: #151b23; border: 1px solid #3d444d; border-radius: 4px 14px 14px 4px;
    box-shadow: inset 18px 0 24px -18px #010409, 0 24px 60px -30px #010409; transform-origin: 0 50%; backface-visibility: hidden; overflow: hidden; }
  .wz-page::before { content: ""; position: absolute; inset: 12px; border: 1px solid #e3b34126; border-radius: 2px 8px 8px 2px; pointer-events: none; }
  .wz-page::after { content: ""; position: absolute; inset: 0; background: linear-gradient(90deg, #01040900, #010409cc); opacity: 0; pointer-events: none; }
  .wz-page.out { z-index: 2; animation: wz-turn .75s cubic-bezier(.45, .05, .35, 1) forwards; pointer-events: none; }
  .wz-page.out::after { animation: wz-shade .75s ease-in forwards; }
  .wz-page.in { animation: wz-rise .75s ease-out; }
  .wz-page.back { z-index: 2; animation: wz-unturn .75s cubic-bezier(.45, .05, .35, 1); }
  .wz-page.back::after { animation: wz-shade .75s ease-out reverse; }
  .wz-page.under { animation: wz-sink .75s ease-in forwards; pointer-events: none; }
  @keyframes wz-turn { to { transform: rotateY(-118deg); } }
  @keyframes wz-unturn { from { transform: rotateY(-118deg); } }
  @keyframes wz-shade { to { opacity: 1; } }
  @keyframes wz-rise { from { filter: brightness(.45); transform: translateZ(-60px); } }
  @keyframes wz-sink { to { filter: brightness(.45); transform: translateZ(-60px); } }
  .wz-scroll { flex: 1; min-height: 0; overflow-y: auto; overscroll-behavior: contain; padding: 40px 56px 24px 64px; }
  .wz-pno { position: absolute; top: 26px; right: 34px; font: 600 12px/1 var(--mono); letter-spacing: .14em; color: var(--gold); }
  .wz-kick { margin: 0 0 10px; font: 600 13px/1 var(--mono); letter-spacing: .14em; text-transform: uppercase; color: #9198a1; }
  .wz-h { margin: 0 0 12px; font: 700 44px/1.08 "Mona Sans", sans-serif; font-variation-settings: "wdth" 100; letter-spacing: -.01em; color: #f0f6fc; outline: none; }
  .wz-lede { margin: 0 0 26px; max-width: 760px; color: #c9d1d9; font-size: 18px; }
  .wz-page .rv { animation: wz-up .6s cubic-bezier(.2, .8, .2, 1) calc(.25s + var(--i, 0) * 45ms) backwards; }
  @keyframes wz-up { from { opacity: 0; transform: translateY(14px); } }
  /* choice cards: "turn to page N" */
  .wz-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: 12px; }
  .wz-grid.big { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .wz-c { position: relative; display: flex; gap: 14px; align-items: flex-start; padding: 16px 16px 16px 14px; text-align: left; background: #0d1117; border: 1px solid #3d444d; border-radius: 10px; cursor: pointer;
    transition: border-color .2s, background .2s, transform .2s; }
  .wz [hidden] { display: none !important; }
  .wz-cell { display: flex; flex-direction: column; }
  .wz-cell > .wz-c { width: 100%; }
  .wz-c:hover { border-color: #9198a1; transform: translateY(-2px); }
  .wz-c[aria-checked="true"] { border-color: var(--gold); background: #e3b3410f; box-shadow: 0 0 0 1px var(--gold), 0 10px 30px -14px #e3b34155; }
  .wz-coin { width: 38px; height: 38px; border-radius: 50%; flex: none; display: grid; place-items: center; border: 1px solid #3d444d; color: #9198a1; font: 700 13px/1 var(--mono); background: #151b23; transition: all .25s; }
  .wz-coin svg.oc { width: 18px; height: 18px; }
  .wz-c[aria-checked="true"] .wz-coin { background: var(--gold); border-color: var(--gold); color: #0d1117; transform: rotateY(360deg); transition: transform .6s, background .25s; }
  .wz-ct { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
  .wz-ct b { font-weight: 600; font-size: 17px; color: #f0f6fc; }
  .wz-ct small { font-size: 14px; line-height: 1.45; color: #9198a1; }
  .wz-ct em { font: 600 11px/1 var(--mono); font-style: normal; letter-spacing: .1em; text-transform: uppercase; color: #9198a1; margin-top: 6px; }
  .wz-c[aria-checked="true"] .wz-ct em { color: var(--gold); }
  .wz-k { position: absolute; top: 10px; right: 12px; font: 600 11px/1 var(--mono); color: #6e7681; }
  .wz-rec { position: absolute; top: -9px; right: 12px; padding: 3px 7px; border-radius: 99px; background: #0d1117; border: 1px solid var(--gold-d); color: var(--gold); font: 600 10px/1 var(--mono); letter-spacing: .1em; text-transform: uppercase; }
  .wz-rec + .wz-k { display: none; }
  .wz-big .wz-c { padding: 22px 20px; }
  .wz-big .wz-coin { width: 48px; height: 48px; }
  .wz-big .wz-coin svg.oc { width: 22px; height: 22px; }
  .wz-big .wz-ct b { font-size: 20px; }
  .wz-big .wz-ct small { font-size: 15px; }
  .wz-more { margin: 18px 0 0; }
  .wz-more > button { display: inline-flex; gap: 8px; align-items: center; padding: 8px 14px; border: 1px dashed #3d444d; border-radius: 8px; background: none; color: #9198a1; cursor: pointer; font: 600 13px/1 var(--mono); letter-spacing: .06em; }
  .wz-more > button:hover { color: #f0f6fc; border-color: #9198a1; }
  .wz-more .wz-grid { margin-top: 14px; }
  .wz-sec { margin: 26px 0 10px; font: 600 12px/1 var(--mono); letter-spacing: .14em; text-transform: uppercase; color: #9198a1; }
  .wz-field { display: flex; flex-direction: column; gap: 6px; margin: 10px 0 0; animation: wz-up .4s ease-out; }
  .wz-field label { font: 600 12px/1.3 var(--mono); letter-spacing: .06em; color: #9198a1; }
  .wz-field input, .wz-field textarea { width: 100%; box-sizing: border-box; padding: 10px 12px; border: 1px solid #3d444d; border-radius: 8px; background: #0d1117; color: #f0f6fc; font: 400 16px/1.5 "Mona Sans", sans-serif; resize: vertical; }
  .wz-field textarea { min-height: 120px; }
  .wz-field input:focus, .wz-field textarea:focus { outline: none; border-color: var(--gold); box-shadow: 0 0 0 3px #e3b34133; }
  .wz-hint { font-size: 14px; color: #9198a1; margin: 8px 0 0; }
  /* page footer: back, dice, turn */
  .wz-foot { display: flex; align-items: center; gap: 12px; padding: 16px 56px 22px 64px; border-top: 1px solid #3d444d; background: #151b23; }
  .wz-b { display: inline-flex; align-items: center; gap: 8px; height: 40px; padding: 0 16px; border-radius: 8px; border: 1px solid #3d444d; background: #0d1117; color: #c9d1d9; cursor: pointer; font: 600 15px/1 "Mona Sans", sans-serif; white-space: nowrap; flex: none; }
  .wz-b:hover:not(:disabled) { border-color: #9198a1; color: #f0f6fc; }
  .wz-b:disabled { opacity: .45; cursor: not-allowed; }
  .wz-b.go { margin-left: auto; background: var(--gold); border-color: var(--gold); color: #0d1117; }
  .wz-b.go:hover:not(:disabled) { color: #0d1117; box-shadow: 0 0 0 4px #e3b34133; }
  .wz-b.go svg.oc { transition: transform .2s; }
  .wz-b.go:hover:not(:disabled) svg.oc { transform: translateX(3px); }
  .wz-why { flex: 1; min-width: 0; font-size: 14px; line-height: 1.35; color: #9198a1; text-align: right; }
  .wz-why:empty { display: none; }
  .wz-why:not(:empty) + .wz-b.go { margin-left: 0; }
  /* the die */
  .wz-die { width: 22px; height: 22px; border-radius: 5px; border: 2px solid currentColor; display: grid; grid-template: repeat(3, 1fr) / repeat(3, 1fr); padding: 2px; box-sizing: border-box; }
  .wz-die i { width: 4px; height: 4px; border-radius: 50%; place-self: center; background: currentColor; opacity: 0; }
  .wz-die i.p { opacity: 1; }
  .wz-b.rolling .wz-die { animation: wz-roll .7s cubic-bezier(.3, .7, .3, 1); }
  @keyframes wz-roll { 0% { transform: rotate(0) scale(1); } 40% { transform: rotate(320deg) scale(1.25); } 100% { transform: rotate(720deg) scale(1); } }
  .wz-c.fated { animation: wz-fate .6s ease-out; }
  @keyframes wz-fate { 0% { box-shadow: 0 0 0 0 #e3b34155; } 100% { box-shadow: 0 0 0 18px #e3b34100; } }
  /* --- the cover --- */
  .wz-cover { position: relative; height: 100%; display: grid; place-items: center; text-align: center; }
  .wz-frame { position: absolute; inset: 40px; pointer-events: none; }
  .wz-frame i { position: absolute; width: 56px; height: 56px; border: 0 solid var(--gold); animation: wz-corner .7s cubic-bezier(.2, .8, .2, 1) .9s backwards; }
  .wz-frame .tl { top: 0; left: 0; border-width: 2px 0 0 2px; transform-origin: 0 0; }
  .wz-frame .tr { top: 0; right: 0; border-width: 2px 2px 0 0; transform-origin: 100% 0; }
  .wz-frame .bl { bottom: 0; left: 0; border-width: 0 0 2px 2px; transform-origin: 0 100%; }
  .wz-frame .br { bottom: 0; right: 0; border-width: 0 2px 2px 0; transform-origin: 100% 100%; }
  @keyframes wz-corner { from { opacity: 0; transform: scale(.2); } }
  .wz-cv { position: relative; max-width: 900px; padding: 0 40px; }
  .wz-cv .wz-kick { color: var(--gold); }
  .wz-title { margin: 6px 0 18px; font: 800 96px/.98 "Mona Sans", sans-serif; font-variation-settings: "wdth" 100; letter-spacing: -.02em; color: #f0f6fc; }
  .wz-title .w { display: inline-block; white-space: nowrap; }
  .wz-title .l { display: inline-block; animation: wz-drop .7s cubic-bezier(.2, 1.4, .4, 1) calc(.4s + var(--i) * 32ms) backwards; }
  .wz-title .g { background: linear-gradient(180deg, #f2cc60, #d29922); -webkit-background-clip: text; background-clip: text; color: transparent; }
  @keyframes wz-drop { from { opacity: 0; transform: translateY(-60px) rotate(-12deg); } }
  .wz-cv p.m { margin: 0 auto 30px; max-width: 640px; font-size: 21px; color: #c9d1d9; }
  .wz-chain { display: flex; justify-content: center; align-items: center; gap: 10px; margin: 0 0 36px; flex-wrap: wrap; }
  .wz-chain span { padding: 8px 14px; border: 1px solid #3d444d; border-radius: 99px; font: 600 12px/1 var(--mono); letter-spacing: .1em; text-transform: uppercase; color: #9198a1; background: #0d1117;
    animation: wz-up .5s ease-out calc(1.3s + var(--i) * .15s) backwards; }
  .wz-chain span.a { color: var(--gold); border-color: var(--gold-d); }
  .wz-chain svg { width: 18px; height: 10px; color: #6e7681; animation: wz-up .5s ease-out calc(1.38s + var(--i) * .15s) backwards; }
  .wz-cover .wz-b.go { margin: 0 auto; height: 52px; padding: 0 26px; font-size: 18px; animation: wz-up .6s ease-out 1.9s backwards, wz-beat 2.4s ease-in-out 3s infinite; }
  @keyframes wz-beat { 0%, 100% { box-shadow: 0 0 0 0 #e3b34100; } 50% { box-shadow: 0 0 0 8px #e3b34133; } }
  .wz-cover .wz-hint { animation: wz-up .6s ease-out 2.1s backwards; }
  /* --- the end --- */
  .wz-end { position: relative; }
  .wz-stamp { float: right; margin: 22px 8px 16px 28px; padding: 8px 18px; border: 3px double var(--gold); color: var(--gold); font: 800 30px/1 var(--mono); letter-spacing: .2em; transform: rotate(-8deg);
    animation: wz-stamp .55s cubic-bezier(.3, 1.6, .5, 1) .35s backwards; }
  @keyframes wz-stamp { from { opacity: 0; transform: rotate(-8deg) scale(2.6); } }
  .wz-scrollp { position: relative; margin: 6px 0 0; border: 1px solid #3d444d; border-radius: 10px; background: #0d1117; animation: wz-unroll 1s cubic-bezier(.3, .7, .2, 1) .5s backwards; }
  @keyframes wz-unroll { from { clip-path: inset(0 0 100% 0); } to { clip-path: inset(0 0 0 0); } }
  .wz-scrollp header { display: flex; justify-content: space-between; padding: 10px 14px; border-bottom: 1px solid #3d444d; font: 600 12px/1 var(--mono); letter-spacing: .1em; text-transform: uppercase; color: #9198a1; }
  .wz-scrollp pre { margin: 0; padding: 14px 16px; max-height: 300px; overflow: auto; font: 400 13px/1.55 var(--mono); color: #c9d1d9; white-space: pre-wrap; overflow-wrap: anywhere; }
  .wz-done { display: none; margin: 18px 0 0; padding: 16px 18px; border: 1px solid var(--gold-d); border-radius: 10px; background: #e3b3410f; animation: wz-up .5s ease-out; }
  .wz-done.on { display: block; }
  .wz-done p { margin: 0; }
  .wz-done .wz-kick { color: var(--gold); margin-bottom: 6px; }
  .wz-done b { display: block; font-size: 20px; margin-bottom: 4px; }
  .wz-spark { position: fixed; width: 6px; height: 6px; background: var(--gold); pointer-events: none; z-index: 9; animation: wz-spark .9s cubic-bezier(.2, .8, .3, 1) forwards; }
  .wz-spark.w { background: #f0f6fc; }
  @keyframes wz-spark { to { opacity: 0; transform: translate(var(--dx), var(--dy)) rotate(var(--r)); } }
  /* phones and narrow windows: the map collapses into a row of chapter pips */
  .wz-pips { display: none; }
  @media (max-width: 900px) {
    .wz-body { grid-template-columns: minmax(0, 1fr); padding: 0 12px 12px; inset: 96px 0 0; }
    .wz-side { display: none; }
    .wz:not(.cover) .wz-pips { display: flex; position: absolute; top: 56px; left: 12px; right: 12px; gap: 6px; align-items: center; justify-content: center; }
    .wz-pips button { width: 26px; height: 26px; padding: 0; border-radius: 50%; border: 2px solid #3d444d; background: #0d1117; color: #9198a1; font: 700 11px/1 var(--mono); }
    .wz-pips button.done { background: var(--gold); border-color: var(--gold); color: #0d1117; }
    .wz-pips button.cur { border-color: var(--gold); color: var(--gold); }
    .wz-pips button:disabled { border-style: dashed; }
    .wz-scroll { padding: 28px 20px 16px 26px; }
    .wz-foot { padding: 12px 16px 14px 22px; gap: 8px; }
    .wz-h { font-size: 30px; }
    .wz-title { font-size: 52px; }
    .wz-grid.big { grid-template-columns: minmax(0, 1fr); }
    .wz-stamp { float: none; display: inline-block; margin: 0 0 12px; font-size: 22px; }
    .wz-why, .wz-sub, .wz-foot .wz-lbl { display: none; }
    .wz-foot .go { margin-left: auto; }
  }
  @media (prefers-reduced-motion: reduce) {
    .wz *, .wz *::before, .wz *::after, .wz[open] { animation: none !important; transition: none !important; }
    .wz-dust { display: none; }
  }`;

  const h = (tag, attrs = {}, ...kids) => {
    const el = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs || {})) {
      if (v == null || v === false) continue;
      if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
      else if (k === "style" && typeof v === "object") for (const [p, x] of Object.entries(v)) el.style.setProperty(p, x);
      else el.setAttribute(k, v === true ? "" : v);
    }
    el.append(...kids.flat(Infinity).filter(k => k != null && k !== false));
    return el;
  };
  const SVGNS = "http://www.w3.org/2000/svg";
  const s = (tag, attrs = {}, ...kids) => {
    const el = document.createElementNS(SVGNS, tag);
    for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
    el.append(...kids.flat().filter(Boolean));
    return el;
  };
  const oc = name => {
    const el = s("svg", { class: "oc", viewBox: "0 0 16 16", "aria-hidden": "true" });
    el.innerHTML = `<path d="${OCTI[name] || OCTI.tools}"/>`;
    return el;
  };
  const reduced = () => matchMedia("(prefers-reduced-motion: reduce)").matches;

  // Chapters: the original wizard's steps, in its order, as gamebook pages.
  const CH = [
    { id: "cover", page: 1, map: "Cover" },
    { id: "intent", page: 7, map: "Prologue", num: "I", name: "The Prologue" },
    { id: "archetype", page: 14, map: "Quest", num: "II", name: "The Quest" },
    { id: "trigger", page: 23, map: "Hour", num: "III", name: "The Hour" },
    { id: "output", page: 42, map: "Mark", num: "IV", name: "The Mark" },
    { id: "extra", page: 58, map: "Pack", num: "V", name: "The Pack" },
    { id: "engine", page: 77, map: "Companion", num: "VI", name: "The Companion" },
    { id: "finish", page: 100, map: "The End", num: "", name: "The End" },
  ];
  const MONO = { copilot: "CP", claude: "CL", codex: "CX", gemini: "GE", pi: "PI", aider: "AI", crush: "CR", cursor: "CU", "deepseek-harness": "DS", goose: "GO", kiro: "KI", "pydantic-ai": "PY", opencode: "OC", factory: "FA", langgraph: "LG", "mini-swe-agent": "MS" };
  const LOCKED = "Choose a quest or write your intent to go on.";
  const FATE = [
    "Keep our docs accurate so new contributors stop tripping over stale setup steps.",
    "Catch flaky tests early so maintainers spend review time on design, not reruns.",
    "Turn every new issue into a labelled, reproducible report within minutes.",
    "Write a short weekly digest so the whole team knows what shipped.",
  ];

  let D, dlg, book, pageEl, mapEl, pipsEl, taleEl, sfx, lit, me, ring, nodes, pips, pos, cur, busy, inkPrev;
  const st = { intent: "", archetype: null, custom: "", triggers: [], outputs: [], extras: [], engine: null, details: {}, rec: { triggers: [], outputs: [] }, seen: new Set([0]), copied: false, more: false, moreEng: false };

  const opts = id => D.wizardOptions(D.wizard, id);
  const step = id => (D.wizard.steps || {})[id] || {};
  const archetypeList = () => {
    const cfg = D.wizard.archetypes || {};
    const pinned = cfg.pinned || [];
    const order = a => {
      const i = pinned.indexOf(a.id);
      return i !== -1 ? i : pinned.indexOf((cfg.aliases || {})[(a.label || "").trim().toLowerCase()]);
    };
    const all = D.patterns.archetypes.filter(a => a.id !== "custom");
    const top = all.filter(a => order(a) !== -1).sort((a, b) => order(a) - order(b));
    return { top, rest: all.filter(a => order(a) === -1) };
  };
  const archIcon = id => (D.getWorkflowDefinition(D.patterns, id) || {}).icon || "tools";
  const engines = () => {
    const builtIn = opts("engine").map(o => o.id);
    const ids = [...builtIn, ...D.engines.map(e => e.id).filter(id => !builtIn.includes(id))];
    const featured = D.featuredEngines.filter(id => ids.includes(id));
    return { featured, rest: ids.filter(id => !featured.includes(id)) };
  };
  const description = () => (st.archetype === "custom" ? st.custom.trim() || st.intent.trim() : "");
  const answers = () => {
    const archetype = st.archetype || "custom";
    const details = [];
    for (const [key, value] of Object.entries(st.details)) {
      const [sid, oid] = key.split(":");
      const list = sid === "trigger" ? st.triggers : sid === "output" ? st.outputs : st.extras;
      const o = opts(sid).find(x => x.id === oid);
      if (!list.includes(oid) || !value.trim() || !o?.detail) continue;
      details.push({ step: sid, option: oid, value: value.trim(), label: o.detail.label || "", requirement: o.detail.requirement || "" });
    }
    return {
      archetype,
      customDescription: st.archetype ? description() : st.intent.trim(),
      triggers: [...st.triggers],
      outputs: [...st.outputs],
      engine: st.engine,
      extras: [...st.extras],
      details,
      intent: st.intent.trim(),
      needsData: D.inferNeedsPreSteps(archetype, D.patterns),
    };
  };
  // Like the original: every chapter past the quest opens once a quest is chosen or an intent is written.
  const open = n => n <= 2 || (st.archetype && st.archetype !== "custom") || !!description() || (!st.archetype && !!st.intent.trim());

  function ensure() {
    if (dlg) return;
    D = window.CracktroWizardData;
    document.head.append(h("style", {}, STYLE));
    dlg = h("dialog", { class: "wz cover", "aria-label": "Workflow wizard: choose your own workflow" });
    const sky = h("div", { class: "wz-sky", "aria-hidden": "true" });
    for (let i = 0; i < 26; i++)
      sky.append(h("i", { class: "wz-dust", style: { left: `${(i * 37) % 100}%`, top: `${40 + ((i * 53) % 60)}%`, "--t": `${7 + (i % 5) * 2}s`, "--d": `${-(i * 0.7)}s`, "--dx": `${((i % 7) - 3) * 18}px` } }));
    const top = h(
      "header",
      { class: "wz-top" },
      h("div", { class: "wz-brand" }, oc("book"), h("b", {}, "Choose your own workflow"), h("span", { class: "wz-sub", "aria-hidden": "true" }, "·"), h("span", { class: "wz-sub" }, "gh aw wizard")),
      h("button", { class: "wz-x", type: "button", title: "Close the wizard (Esc)", "aria-label": "Close the wizard (Esc)", "aria-keyshortcuts": "Escape", onclick: () => dlg.close() }, oc("x"))
    );
    pipsEl = h("nav", { class: "wz-pips", "aria-label": "Chapters" });
    mapEl = h("nav", { class: "wz-map", "aria-label": "Trail map" });
    taleEl = h("section", { class: "wz-tale", "aria-label": "The tale so far", "aria-live": "polite" });
    book = h("main", { class: "wz-book" });
    dlg.append(sky, top, pipsEl, h("div", { class: "wz-body" }, h("aside", { class: "wz-side" }, mapEl, taleEl), book));
    buildMap();
    dlg.addEventListener("keydown", onKey);
    dlg.addEventListener("click", e => e.stopPropagation());
    for (const t of ["touchstart", "touchend"]) dlg.addEventListener(t, e => e.stopPropagation(), { passive: true });
    dlg.addEventListener("cancel", e => {
      e.preventDefault();
      dlg.close();
    });
    dlg.addEventListener("close", () => document.activeElement?.blur?.());
    document.body.append(dlg);
  }

  // ---------- trail map ----------
  function buildMap() {
    pos = CH.slice(1).map((_, i) => [150 + Math.sin(i * 1.15 + 0.4) * 78, 24 + i * 62]);
    const svg = s("svg", { class: "trail", viewBox: "0 0 300 420", width: 300, height: 420, "aria-hidden": "true" });
    const seg = (a, b) => {
      const my = (a[1] + b[1]) / 2;
      return `M${a[0]} ${a[1]}C${a[0]} ${my} ${b[0]} ${my} ${b[0]} ${b[1]}`;
    };
    lit = [];
    for (let i = 0; i < pos.length - 1; i++) {
      svg.append(s("path", { class: "wz-seg", d: seg(pos[i], pos[i + 1]) }));
      const l = s("path", { class: "wz-lit", d: seg(pos[i], pos[i + 1]), pathLength: 1 });
      lit.push(l);
      svg.append(l);
    }
    ring = s("circle", { class: "wz-me-ring", r: 9 });
    me = s("circle", { class: "wz-me", r: 5 });
    const g = s("g", {}, ring, me);
    svg.append(g);
    me._g = g;
    mapEl.append(svg);
    nodes = CH.slice(1).map((c, i) => {
      const b = h(
        "button",
        { class: `wz-node${pos[i][0] < 150 ? " left" : ""}`, type: "button", style: { left: `${pos[i][0]}px`, top: `${pos[i][1]}px` }, onclick: () => turn(i + 1) },
        h("i", {}, c.num || "✦"),
        h("span", {}, c.map)
      );
      mapEl.append(b);
      return b;
    });
    pips = CH.slice(1).map((c, i) => {
      const b = h("button", { type: "button", onclick: () => turn(i + 1) }, c.num || "✦");
      pipsEl.append(b);
      return b;
    });
  }
  function paintMap(from) {
    CH.slice(1).forEach((c, i) => {
      const n = i + 1;
      const can = open(n);
      const label = `Page ${c.page}: ${c.name}${can ? "" : " (choose a quest or write your intent first)"}`;
      for (const b of [nodes[i], pips[i]]) {
        b.disabled = !can;
        b.classList.toggle("cur", n === cur);
        b.classList.toggle("seen", st.seen.has(n));
        b.classList.toggle("done", st.seen.has(n) && n < cur);
        b.title = label;
        b.setAttribute("aria-label", label);
        if (n === cur) b.setAttribute("aria-current", "step");
        else b.removeAttribute("aria-current");
      }
    });
    lit.forEach((l, i) => l.classList.toggle("on", i + 1 < cur));
    walk(from);
  }
  // The traveller walks the trail segment by segment to the current chapter.
  function walk(from) {
    const to = Math.max(1, cur) - 1;
    const at = p => me._g.setAttribute("transform", `translate(${p.x} ${p.y})`);
    const start = from == null ? to : Math.max(0, from - 1);
    if (reduced() || start === to || !dlg.open) return at({ x: pos[to][0], y: pos[to][1] });
    const dir = to > start ? 1 : -1;
    const steps = [];
    for (let i = start; i !== to; i += dir) steps.push(dir > 0 ? [lit[i], 0, 1] : [lit[i - 1], 1, 0]);
    const per = 700 / steps.length,
      t0 = performance.now();
    const tick = now => {
      const k = Math.min(steps.length, (now - t0) / per);
      const i = Math.min(steps.length - 1, Math.floor(k));
      const [path, a, b] = steps[i];
      const f = k - i,
        e = f < 0.5 ? 2 * f * f : 1 - (-2 * f + 2) ** 2 / 2;
      const len = path.getTotalLength();
      at(path.getPointAtLength(len * (a + (b - a) * (k >= steps.length ? 1 : e))));
      if (k < steps.length) requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  }

  // ---------- the tale so far ----------
  function paintTale() {
    const sum = D.buildWorkflowSummary(answers(), D.patterns, D.wizard);
    const prev = inkPrev || {};
    const bit = (k, lower) => {
      const v = sum[k];
      const raw = String(v.value).replace(/[.!?]+$/, "");
      const text = lower ? raw.charAt(0).toLowerCase() + raw.slice(1) : raw;
      return h("span", { class: v.complete ? (prev[k] !== v.value ? "ink" : "") : "todo" }, text);
    };
    inkPrev = Object.fromEntries(Object.entries(sum).map(([k, v]) => [k, v.complete ? v.value : null]));
    taleEl.replaceChildren(
      h("h3", {}, "The tale so far"),
      h(
        "p",
        {},
        "You set out to ",
        bit("purpose", true),
        ". When ",
        bit("trigger"),
        ", ",
        bit("engine"),
        " wakes and may ",
        bit("output", true),
        ", carrying ",
        bit("extras"),
        ".",
        sum.intent.complete ? [" ", h("i", {}, "Because: "), bit("intent")] : null
      )
    );
  }

  // ---------- pages ----------
  function card({ id, icon, mono, label, desc, sel, rec, key, onpick, role = "checkbox", extra }) {
    const b = h(
      "button",
      { class: "wz-c rv", type: "button", role, "aria-checked": String(!!sel), "data-id": id, style: { "--i": key || 0 }, title: `${label}${key && key < 10 ? ` (${key})` : " (Enter)"}`, onclick: onpick },
      h("span", { class: "wz-coin", "aria-hidden": "true" }, icon ? oc(icon) : mono),
      h("span", { class: "wz-ct" }, h("b", {}, label), desc ? h("small", {}, desc) : null, extra ? h("em", {}, extra) : null),
      rec ? h("span", { class: "wz-rec" }, "★ Map says") : null,
      key && key < 10 ? h("span", { class: "wz-k", "aria-hidden": "true" }, key) : null
    );
    if (key && key < 10) b.setAttribute("aria-keyshortcuts", String(key));
    return b;
  }
  function detailField(sid, o) {
    if (!o.detail) return null;
    const key = `${sid}:${o.id}`;
    const fid = `wz-d-${sid}-${o.id}`;
    return h(
      "div",
      { class: "wz-field" },
      h("label", { for: fid }, o.detail.label || `Add details for ${o.label}`),
      h("input", { id: fid, type: "text", value: st.details[key] || "", placeholder: o.detail.placeholder || "", oninput: e => ((st.details[key] = e.target.value), paintTale()) })
    );
  }
  function head(c, title, lede) {
    return [
      h("div", { class: "wz-pno", "aria-hidden": "true" }, `PAGE ${c.page}`),
      h("p", { class: "wz-kick rv", style: { "--i": 0 } }, `Chapter ${c.num} · ${c.name}`),
      h("h2", { class: "wz-h rv", tabindex: "-1", style: { "--i": 1 } }, title),
      lede ? h("p", { class: "wz-lede rv", style: { "--i": 2 } }, lede) : null,
    ];
  }
  function foot(n, { dice, auto } = {}) {
    const next = CH[n + 1];
    const can = open(n + 1);
    return h(
      "footer",
      { class: "wz-foot" },
      h("button", { class: "wz-b", type: "button", title: "Back a page (←)", "aria-keyshortcuts": "ArrowLeft", onclick: () => turn(n - 1) }, oc("arrow-left"), h("span", { class: "wz-lbl" }, "Back")),
      dice ? h("button", { class: "wz-b dice", type: "button", title: "Let fate decide: roll the die (R)", "aria-keyshortcuts": "R", onclick: e => roll(e.currentTarget) }, die(st.lastRoll || 5), h("span", { class: "wz-lbl" }, "Let fate decide")) : null,
      h("span", { class: "wz-why", "data-auto": auto ? "Choose to turn the page." : "" }, !can ? LOCKED : auto ? "Choose to turn the page." : ""),
      h(
        "button",
        { class: "wz-b go", type: "button", disabled: !can, title: `Turn to page ${next.page} (→)`, "aria-keyshortcuts": "ArrowRight", onclick: () => turn(n + 1) },
        next.id === "finish" ? "Read the ending" : `Turn to page ${next.page}`,
        oc("arrow-right")
      )
    );
  }
  const PIPS = { 1: [4], 2: [0, 8], 3: [0, 4, 8], 4: [0, 2, 6, 8], 5: [0, 2, 4, 6, 8], 6: [0, 2, 3, 5, 6, 8] };
  const die = v => h("span", { class: "wz-die", "aria-hidden": "true" }, Array.from({ length: 9 }, (_, i) => h("i", { class: PIPS[v].includes(i) ? "p" : "" })));

  function pageFor(n) {
    const c = CH[n];
    const pg = h("article", { class: "wz-page", "aria-label": c.id === "cover" ? "Cover" : `Page ${c.page}: ${c.name}` });
    const body = h("div", { class: "wz-scroll" });
    let footEl = null;
    if (c.id === "cover") return cover(pg);
    if (c.id === "intent") {
      const it = step("intent");
      const ta = h("textarea", { id: "wz-intent", placeholder: it.field_placeholder || "", oninput: e => ((st.intent = e.target.value), refresh()) }, st.intent);
      body.append(
        ...head(c, "Why do you set out?", "Every adventure starts with a reason. Say what outcome you want, and the tale ends with graders and evals that check it. You may also skip the prologue."),
        h("div", { class: "wz-field rv", style: { "--i": 3 } }, h("label", { for: "wz-intent" }, it.label || "Your intent"), ta),
        it.hint ? h("p", { class: "wz-hint rv", style: { "--i": 4 } }, it.hint) : null
      );
      footEl = foot(n, { dice: true });
    } else if (c.id === "archetype") {
      const { top, rest } = archetypeList();
      let k = 0;
      const pick = id => () => chooseArchetype(id);
      const mk = (a, big) =>
        card({ id: a.id, icon: archIcon(a.id), label: a.label, desc: a.description, sel: st.archetype === a.id, role: "radio", key: big ? ++k : 0, onpick: pick(a.id) });
      const more = h(
        "div",
        { class: "wz-more" },
        h("button", { type: "button", "aria-expanded": String(st.more), title: "Show every quest (Enter)", onclick: e => ((st.more = !st.more), e.currentTarget.setAttribute("aria-expanded", String(st.more)), e.currentTarget.nextElementSibling.toggleAttribute("hidden")) }, oc("milestone"), `Other paths through the woods (${rest.length})`),
        h("div", { class: "wz-grid", role: "radiogroup", "aria-label": "Other quests", hidden: !st.more || null }, rest.map(a => mk(a)))
      );
      if (rest.some(a => a.id === st.archetype)) ((st.more = true), more.lastChild.removeAttribute("hidden"), more.firstChild.setAttribute("aria-expanded", "true"));
      const tops = top.map(a => mk(a, true));
      const custom = card({ id: "custom", icon: "pencil", label: "Forge your own quest", desc: "Describe a workflow no map has drawn yet.", sel: st.archetype === "custom", role: "radio", key: ++k, onpick: pick("custom") });
      body.append(
        ...head(c, "Which quest calls to you?", "Each path is a pattern seen in many repositories. Choose one and the map marks the usual route: when it runs and what it may write."),
        h("div", { class: "wz-grid big wz-big", role: "radiogroup", "aria-label": "Quests" }, tops, custom),
        st.archetype === "custom"
          ? h(
              "div",
              { class: "wz-field" },
              h("label", { for: "wz-custom" }, "Your quest"),
              h("textarea", { id: "wz-custom", placeholder: "e.g., Summarize new discussions every morning and flag the urgent ones", oninput: e => ((st.custom = e.target.value), refresh()) }, st.custom || st.intent)
            )
          : null,
        more
      );
      footEl = foot(n, { dice: true, auto: !st.archetype });
    } else if (c.id === "trigger" || c.id === "output" || c.id === "extra") {
      const key = { trigger: "triggers", output: "outputs", extra: "extras" }[c.id];
      const text = {
        trigger: ["When does your agent wake?", "Choose what starts each run. A fuzzy schedule spreads runs out so a thousand repositories never wake at the same minute."],
        output: ["What may it leave behind?", "The agent itself works read-only. Safe outputs are the only marks it can make, and each one is checked in a separate job before it lands."],
        extra: ["What goes in your pack?", "Optional gear for the road. Travel light: each tool is one more thing the agent can reach for."],
      }[c.id];
      const rec = c.id === "trigger" ? st.rec.triggers : c.id === "output" ? st.rec.outputs : [];
      const grid = h("div", { class: "wz-grid", role: "group", "aria-label": c.name });
      opts(c.id).forEach((o, i) => {
        const sel = st[key].includes(o.id);
        grid.append(
          h(
            "div",
            { class: "wz-cell" },
            card({ id: o.id, icon: o.icon, label: o.label, desc: o.description, sel, rec: rec.includes(o.id), key: i + 1, onpick: () => toggle(key, o.id) }),
            sel ? detailField(c.id, o) : null
          )
        );
      });
      body.append(...head(c, ...text), grid);
      footEl = foot(n, { dice: true });
    } else if (c.id === "engine") {
      const { featured, rest } = engines();
      const company = id => {
        const l = D.formatEngineOptionLabel(id);
        const co = /\((.+)\)$/.exec(l)?.[1];
        return co === "Extension" ? undefined : co;
      };
      let k = 0;
      const mk = id =>
        card({ id, mono: MONO[id] || id.slice(0, 2).toUpperCase(), label: D.formatEngineLabel(id), desc: (opts("engine").find(o => o.id === id) || {}).description, extra: company(id), sel: st.engine === id, role: "radio", key: ++k, onpick: () => chooseEngine(id) });
      const showRest = st.moreEng || rest.includes(st.engine);
      body.append(
        ...head(c, "Who walks the path with you?", "Pick the coding agent that runs the workflow. Changing companions later is a one-line edit to the engine field."),
        h("div", { class: "wz-grid big wz-big", role: "radiogroup", "aria-label": "Companions" }, featured.map(mk)),
        h(
          "div",
          { class: "wz-more" },
          h("button", { type: "button", "aria-expanded": String(showRest), title: "Show more companions (Enter)", onclick: e => ((st.moreEng = !st.moreEng), e.currentTarget.setAttribute("aria-expanded", String(st.moreEng)), e.currentTarget.nextElementSibling.toggleAttribute("hidden")) }, oc("plug"), `More companions (${rest.length})`),
          h("div", { class: "wz-grid", role: "radiogroup", "aria-label": "More companions", hidden: !showRest || null }, rest.map(mk))
        )
      );
      footEl = foot(n, { dice: true, auto: !st.engine });
    } else return ending(pg, c);
    pg.append(body, footEl);
    return pg;
  }

  function cover(pg) {
    const L = D.wizard.landing || {};
    let i = 0;
    const word = (w, g) => h("span", { class: "w" }, [...w].map(ch => h("span", { class: `l${g ? " g" : ""}`, style: { "--i": i++ } }, ch)));
    const title = h("h1", { class: "wz-title", "aria-label": "Choose your own workflow" }, word("Choose"), " ", word("your"), " ", word("own"), h("br"), word("workflow", true));
    const frame = h("div", { class: "wz-frame", "aria-hidden": "true" }, ...["tl", "tr", "bl", "br"].map(c => h("i", { class: c })));
    const arrow = j => {
      const a = s("svg", { viewBox: "0 0 18 10", style: `--i:${j}`, "aria-hidden": "true" });
      a.innerHTML = '<path d="M0 5h15M11 1l4 4-4 4" fill="none" stroke="currentColor" stroke-width="1.5"/>';
      return a;
    };
    const chain = h(
      "div",
      { class: "wz-chain", "aria-label": `${L.trigger_label} ${L.trigger_description}, ${L.agent_label}, ${L.outputs_label}` },
      h("span", { style: { "--i": 0 } }, L.trigger_label || "When"),
      arrow(0),
      h("span", { class: "a", style: { "--i": 1 } }, L.agent_label || "Agent"),
      arrow(1),
      h("span", { style: { "--i": 2 } }, L.outputs_label || "Safe outputs")
    );
    pg.append(
      h(
        "div",
        { class: "wz-cover" },
        frame,
        h(
          "div",
          { class: "wz-cv" },
          h("p", { class: "wz-kick rv" }, "A GitHub Agentic Workflows adventure"),
          title,
          h("p", { class: "m rv", style: { "--i": 6 } }, L.message || ""),
          chain,
          h("button", { class: "wz-b go", type: "button", title: "Open the book (Enter)", onclick: () => turn(1) }, oc("book"), "Open the book", oc("arrow-right")),
          h("p", { class: "wz-hint" }, `${L.hint || ""} ${L.runner || ""}`.trim())
        )
      )
    );
    return pg;
  }

  function ending(pg, c) {
    const F = D.wizard.finish || {},
      S = D.wizard.copy_success || {};
    const prompt = D.generateAgentPrompt(answers(), D.patterns);
    const copy = h("button", { class: "wz-b go", type: "button", title: `${F.copy_button || "Copy prompt"} (C)`, "aria-keyshortcuts": "C", onclick: e => copyPrompt(prompt, e.currentTarget, done) }, oc("copy"), F.copy_button || "Copy prompt");
    const done = h(
      "div",
      { class: `wz-done${st.copied ? " on" : ""}`, role: "status" },
      h("p", { class: "wz-kick" }, S.eyebrow || "Prompt copied"),
      h("b", {}, S.title || ""),
      h("p", {}, S.description || ""),
      h("p", { class: "wz-hint" }, `${S.next_step_title || ""}: ${S.next_step_description || ""}`)
    );
    pg.append(
      h(
        "div",
        { class: "wz-scroll wz-end" },
        h("div", { class: "wz-pno", "aria-hidden": "true" }, `PAGE ${c.page}`),
        h("div", { class: "wz-stamp", "aria-hidden": "true" }, "THE END"),
        h("p", { class: "wz-kick rv" }, `${F.step_label || "Finish"} · ${F.step_description || ""}`),
        h("h2", { class: "wz-h rv", tabindex: "-1", style: { "--i": 1 } }, F.title || "Your prompt is ready"),
        h("p", { class: "wz-lede rv", style: { "--i": 2 } }, F.message || ""),
        h("section", { class: "wz-scrollp", "aria-label": F.preview_aria_label || "Generated prompt preview" }, h("header", {}, h("span", {}, F.preview_label || "Prompt preview"), h("span", {}, F.preview_hint || "")), h("pre", { tabindex: "0" }, prompt)),
        done
      ),
      h(
        "footer",
        { class: "wz-foot" },
        h("button", { class: "wz-b", type: "button", title: "Back a page (←)", "aria-keyshortcuts": "ArrowLeft", onclick: () => turn(cur - 1) }, oc("arrow-left"), "Back"),
        h("button", { class: "wz-b", type: "button", title: "Start a new adventure (N)", "aria-keyshortcuts": "N", onclick: restart }, oc("book"), "New adventure"),
        copy
      )
    );
    return pg;
  }

  // ---------- choices ----------
  function chooseArchetype(id) {
    sfx?.touch?.();
    if (st.archetype === id) {
      st.archetype = null;
      st.triggers = [];
      st.outputs = [];
      st.extras = [];
      st.rec = { triggers: [], outputs: [] };
      return rerender();
    }
    st.archetype = id;
    if (id === "custom") {
      st.rec = { triggers: [], outputs: [] };
      return rerender(() => document.getElementById("wz-custom")?.focus());
    }
    st.custom = "";
    const r = D.getRecommendedConfiguration(D.patterns, id, D.wizard);
    st.rec = { triggers: r.triggers, outputs: r.outputs };
    st.triggers = [...r.triggers];
    st.outputs = [...r.outputs];
    st.extras = [];
    mark(id);
    setTimeout(() => cur === 2 && st.archetype === id && turn(3), reduced() ? 0 : 520);
  }
  function chooseEngine(id) {
    sfx?.touch?.();
    st.engine = st.engine === id ? null : id;
    mark(st.engine);
    if (st.engine) setTimeout(() => cur === 6 && st.engine === id && turn(7), reduced() ? 0 : 520);
  }
  function toggle(key, id) {
    sfx?.touch?.();
    const list = st[key];
    const i = list.indexOf(id);
    if (i === -1) list.push(id);
    else list.splice(i, 1);
    rerender(() => pageEl.querySelector(`[data-id="${CSS.escape(id)}"]`)?.focus());
  }
  // Repaint the selection without flipping the page.
  function mark(id) {
    const pg = pageEl;
    for (const b of pg.querySelectorAll(".wz-c")) b.setAttribute("aria-checked", String(b.dataset.id === id));
    refresh();
  }
  function rerender(after) {
    const old = pageEl;
    const top = old?.querySelector(".wz-scroll")?.scrollTop || 0;
    const pg = pageFor(cur);
    for (const el of pg.querySelectorAll(".rv")) el.classList.remove("rv");
    old?.replaceWith(pg);
    pageEl = pg;
    const sc = pg.querySelector(".wz-scroll");
    if (sc) sc.scrollTop = top;
    refresh();
    after?.();
  }
  // Keep the turn button, map and tale in step with typed text without rebuilding the page.
  function refresh() {
    paintTale();
    paintMap();
    const pg = pageEl;
    const go = pg?.querySelector(".wz-foot .wz-b.go");
    if (go && cur < 7) {
      const can = open(cur + 1);
      go.disabled = !can;
      const why = pg.querySelector(".wz-why");
      if (why) why.textContent = can ? why.dataset.auto : LOCKED;
    }
  }

  function roll(btn) {
    const pg = pageEl;
    const c = CH[cur];
    const v = 1 + Math.floor(Math.random() * 6);
    st.lastRoll = v;
    btn.classList.remove("rolling");
    void btn.offsetWidth;
    btn.classList.add("rolling");
    sfx?.step?.(1);
    let t = 0;
    const face = btn.querySelector(".wz-die");
    const spin = setInterval(() => {
      face.replaceWith(die(1 + ((t++ * 5 + v) % 6)));
      if (t > 6) clearInterval(spin);
    }, 90);
    setTimeout(
      () => {
        clearInterval(spin);
        btn.querySelector(".wz-die").replaceWith(die(v));
        if (c.id === "intent") {
          st.intent = FATE[v % FATE.length];
          const ta = pg.querySelector("#wz-intent");
          ta.value = st.intent;
          return refresh();
        }
        if (c.id === "archetype") {
          const { top, rest } = archetypeList();
          const pool = [...top, ...rest];
          return fated(pool[Math.floor(Math.random() * pool.length)].id);
        }
        if (c.id === "engine") {
          const { featured, rest } = engines();
          const pool = [...featured, ...rest];
          return fated(pool[Math.floor(Math.random() * pool.length)]);
        }
        const key = { trigger: "triggers", output: "outputs", extra: "extras" }[c.id];
        const all = opts(c.id).map(o => o.id);
        const pickN = c.id === "extra" ? 1 : 1 + (v > 4 ? 1 : 0);
        st[key] = [...all].sort(() => Math.random() - 0.5).slice(0, pickN);
        rerender();
        for (const id of st[key]) pageEl.querySelector(`[data-id="${CSS.escape(id)}"]`)?.classList.add("fated");
      },
      reduced() ? 0 : 640
    );
  }
  function fated(id) {
    const pg = pageEl;
    let el = pg.querySelector(`[data-id="${CSS.escape(id)}"]`);
    if (el?.closest("[hidden]")) {
      el.closest("[hidden]").removeAttribute("hidden");
      el = pg.querySelector(`[data-id="${CSS.escape(id)}"]`);
    }
    el?.scrollIntoView({ block: "nearest", behavior: reduced() ? "auto" : "smooth" });
    el?.classList.add("fated");
    el?.click();
  }

  async function copyPrompt(text, btn, done) {
    const F = D.wizard.finish || {};
    let ok = false;
    try {
      await navigator.clipboard.writeText(text);
      ok = true;
    } catch {
      const ta = h("textarea", { style: { position: "fixed", opacity: "0" } }, text);
      dlg.append(ta);
      ta.select();
      try {
        ok = document.execCommand("copy");
      } catch {
        ok = false;
      }
      ta.remove();
    }
    btn.lastChild.textContent = ok ? (D.wizard.copy_success || {}).eyebrow || "Prompt copied" : F.copy_failure_button || "Copy failed, try again";
    if (!ok) return;
    st.copied = true;
    done.classList.add("on");
    sfx?.step?.(1);
    burst(btn);
  }
  function burst(btn) {
    if (reduced()) return;
    const r = btn.getBoundingClientRect();
    for (let i = 0; i < 36; i++) {
      const a = (i / 36) * Math.PI * 2 + Math.random() * 0.3,
        d = 80 + Math.random() * 140;
      const p = h("i", {
        class: `wz-spark${i % 4 ? "" : " w"}`,
        style: { left: `${r.left + r.width / 2}px`, top: `${r.top + r.height / 2}px`, "--dx": `${Math.cos(a) * d}px`, "--dy": `${Math.sin(a) * d - 40}px`, "--r": `${Math.random() * 360}deg` },
      });
      dlg.append(p);
      setTimeout(() => p.remove(), 1000);
    }
  }

  function restart() {
    Object.assign(st, { intent: "", archetype: null, custom: "", triggers: [], outputs: [], extras: [], engine: null, details: {}, rec: { triggers: [], outputs: [] }, seen: new Set([0]), copied: false, more: false, moreEng: false });
    inkPrev = null;
    turn(0, true);
  }

  // ---------- page turning ----------
  function turn(n, force) {
    if (n < 0 || n >= CH.length || (n === cur && !force) || (!open(n) && !force) || busy) return;
    const from = cur;
    const fwd = from == null || n > from;
    cur = n;
    st.seen.add(n);
    dlg.classList.toggle("cover", n === 0);
    const old = pageEl;
    const pg = (pageEl = pageFor(n));
    if (old && !reduced()) {
      busy = true;
      if (fwd) {
        book.insertBefore(pg, old);
        pg.classList.add("in");
        old.classList.add("out");
      } else {
        book.append(pg);
        pg.classList.add("back");
        old.classList.add("under");
      }
      sfx?.step?.(fwd ? 1 : -1);
      setTimeout(() => {
        old.remove();
        // re-appending moves the node, which drops focus: put it back
        const f = pg.contains(document.activeElement) ? document.activeElement : null;
        if (fwd && pg.isConnected) book.append(pg);
        f?.focus({ preventScroll: true });
        pageEl.classList.remove("in", "back");
        busy = false;
      }, 760);
    } else {
      old?.remove();
      book.append(pg);
    }
    if (from != null) sfx?.touch?.();
    paintTale();
    paintMap(from);
    requestAnimationFrame(() => (pg.querySelector(".wz-h") || pg.querySelector(".wz-b.go"))?.focus({ preventScroll: true }));
  }

  function onKey(e) {
    e.stopPropagation();
    const typing = e.target.closest?.("input, textarea, select, [contenteditable]");
    if (e.key === "Escape") {
      e.preventDefault();
      if (typing) return e.target.blur();
      return dlg.close();
    }
    if (typing || e.metaKey || e.ctrlKey || e.altKey) return;
    const pg = pageEl;
    if (e.key === "ArrowRight" || e.key === "PageDown") {
      e.preventDefault();
      if (cur === 0) turn(1);
      else if (cur < 7 && open(cur + 1)) turn(cur + 1);
    } else if (e.key === "ArrowLeft" || e.key === "PageUp") {
      e.preventDefault();
      if (cur > 0) turn(cur - 1);
    } else if ((e.key === "r" || e.key === "R") && cur > 0 && cur < 7) {
      e.preventDefault();
      pg.querySelector(".wz-b.dice")?.click();
    } else if ((e.key === "c" || e.key === "C") && cur === 7) {
      e.preventDefault();
      pg.querySelector(".wz-b.go")?.click();
    } else if ((e.key === "n" || e.key === "N") && cur === 7) {
      e.preventDefault();
      restart();
    } else if ((e.key === "ArrowDown" || e.key === "ArrowUp") && e.target.closest?.(".wz-c")) {
      // move between the visible choice cards (← and → turn pages)
      e.preventDefault();
      const cs = [...pg.querySelectorAll(".wz-c")].filter(b => !b.closest("[hidden]"));
      const i = cs.indexOf(e.target.closest(".wz-c"));
      cs[(i + (e.key === "ArrowDown" ? 1 : -1) + cs.length) % cs.length]?.focus();
    } else if (/^[1-9]$/.test(e.key) && cur > 0 && cur < 7) {
      e.preventDefault();
      pg.querySelector(`.wz-c[aria-keyshortcuts="${e.key}"]`)?.click();
    } else if (e.key === "Enter" && cur === 0 && !e.target.closest?.("button")) {
      e.preventDefault();
      turn(1);
    }
  }

  window.CracktroWizard = {
    title: "Workflow wizard",
    key: "W",
    isOpen: () => !!dlg?.open,
    open({ sound } = {}) {
      if (!window.CracktroWizardData) return;
      ensure();
      sfx = sound;
      if (dlg.open) return;
      dlg.showModal();
      if (cur == null) turn(0, true);
      else {
        paintTale();
        paintMap();
        requestAnimationFrame(() => pageEl?.querySelector(".wz-h, .wz-b.go")?.focus({ preventScroll: true }));
      }
    },
    close: () => dlg?.open && dlg.close(),
  };
})();
