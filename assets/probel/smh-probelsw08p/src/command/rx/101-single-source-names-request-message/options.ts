import { LengthOfNamesRequired } from '../../shared/length-of-names-required';

/**
 * SingleSourceNamesRequestMessage command options
 *
 * @export
 * @interface LengthOfNamesRequiredCommandOptions
 */
export interface SingleSourceNamesRequestMessageCommandOptions {
    /**
     * Length of Names Required
     *
     * @type {LengthOfNamesRequired}
     * @memberof SingleSourceNamesRequestMessageCommandOptions
     */
    lengthOfNames: LengthOfNamesRequired;
}

/**
 * Utility class providing some extra functionality on the SingleSourceNamesRequestMessage command options
 *
 * @export
 * @class CommandOptionsUtility
 */
export class CommandOptionsUtility {
    /**
     * Gets a textual representation of the command options
     *
     * @param {SingleSourceNamesRequestMessageCommandOptions} options the command options
     * @returns {string} the command options textual representation
     */
    static toString(options: SingleSourceNamesRequestMessageCommandOptions): string {
        return `lengthOfNames:${this.toStringLengthOfNamesRequired(options.lengthOfNames)}`;
    }

    /**
     * Gets a textual representation of the LengthOfNamesRequired
     *
     *
     * @param {MaintenanceFunction} data the LengthOfNamesRequired
     * @returns {string} the LengthOfNamesRequired textual representation
     * @memberof CommandOptionsUtility
     */
    static toStringLengthOfNamesRequired(data: LengthOfNamesRequired): string {
        return `( ${data} - ${LengthOfNamesRequired[data]} )`;
    }
}
