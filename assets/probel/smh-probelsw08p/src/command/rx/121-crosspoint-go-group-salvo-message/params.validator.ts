import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { IValidator } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CrossPointGoGroupSalvoMessageCommandParams } from './params';

/**
 * CrossPointGoGroupSalvoMessageCommandParams command parameters validator
 *
 * @export
 * @class CommandParamsValidator
 * @implements {IValidator<CrossPointGoGroupSalvoMessageCommandParams>}
 */
export class CommandParamsValidator implements IValidator<CrossPointGoGroupSalvoMessageCommandParams> {
    /**
     * Creates an instance of CommandParamsValidator
     *
     * @param {CrossPointGoGroupSalvoMessageCommandParams} data the command parameters
     * @memberof CommandParamsValidator
     */
    constructor(readonly data: CrossPointGoGroupSalvoMessageCommandParams) {}

    /**
     * Validates the command parameter(s) and returns one error message for each invalid parameter
     *
     * @returns {Record<string, LocaleData>} the localized validation errors
     * @memberof CommandParamsValidator
     */
    validate(): Record<string, LocaleData> {
        const errors: Record<string, any> = {};
        const cache = LocaleDataCache.INSTANCE;

        if (this.isSalvoIdOutOfRange()) {
            errors.salvoId = cache.getCommandErrorLocaleData(CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        return errors;
    }

    /**
     * Gets a boolean indicating whether the salvoId is out of range
     * + false = salvoId is included within the limits
     * + true = salvoId [0-127] is out of range
     *
     * @private
     * @returns {boolean} 'true' if salvoId is out of range; otherwise 'false'
     * @memberof CommandParamsValidator
     */
    private isSalvoIdOutOfRange(): boolean {
        return this.data.salvoId < 0 || this.data.salvoId > 127;
    }
}
