/**
 * Defines the shape of locale data (where description is maintained in multiple language)
 *
 * @export
 * @interface LocaleData
 */
export interface LocaleData {
    /**
     * Uniquely identifies a Locale Data cross Locale Data file (per LocaleId)
     *
     * @type {string}
     * @memberof LocaleData
     */
    id: string;

    /**
     * Locale Data description
     *
     * @type {string}
     * @memberof LocaleData
     */
    description: string;
}
