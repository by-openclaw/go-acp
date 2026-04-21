import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandOptionsUtility, CrossPointGoGroupSalvoMessageCommandOptions } from './options';
import { CommandParamsUtility, CrossPointGoGroupSalvoMessageCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the CrossPoint Go Group Salvo Message command
 *
 * This message is issued by the remote device to triggers the receiving device to set all routes in / clear the previously received CROSSPOINT CONNECT ON GO GROUP SALVO messages.
 * A CROSSPOINT GO DONE GROUP SALVO ACKNOWLEDGE message (Command Byte 123) will be issued to indicate that the command has been executed.
 *
 * Note : No individual CONNECTED messages (Command Byte 04) are issued.  It is the responsibility of the controlling / listening devices to use the CROSSPOINT CONNECT ON GO GROUP SALVO ACKNOWLEDGE (Command Byte 122) and
 * CROSSPOINT GO DONE GROUP SALVO (Command Byte 123) to keep their tally Information up to date.
 * N.B. 	The group salvo commands are only implemented on the XD and ECLIPSE router ranges.
 *
 * Command issued by the remote device
 * @export
 * @class CrossPointGoGroupSalvoMessageCommand
 * @extends {CommandBase<CrossPointGoGroupSalvoMessageCommandParams>}
 */
export class CrossPointGoGroupSalvoMessageCommand extends CommandBase<
    CrossPointGoGroupSalvoMessageCommandParams,
    CrossPointGoGroupSalvoMessageCommandOptions
> {
    /**
     * Creates an instance of CrossPointGoGroupSalvoMessageCommand
     *
     * @param {CrossPointGoGroupSalvoMessageCommandParams} params the command parameters
     * @param {CrossPointGoGroupSalvoMessageCommandOptions} _options the command options
     * @memberof CrossPointGoGroupSalvoMessageCommand
     */
    constructor(
        params: CrossPointGoGroupSalvoMessageCommandParams,
        options: CrossPointGoGroupSalvoMessageCommandOptions
    ) {
        super(CommandIdentifiers.RX.GENERAL.CROSSPOINT_GO_GROUP_SALVO_MESSAGE, params, options);
        this.validateParams(params);
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual description
     * @memberof CrossPointGoGroupSalvoMessageCommand
     */
    toLogDescription(): string {
        return `General - ${this.name}: ${CommandParamsUtility.toString(this.params)}, ${CommandOptionsUtility.toString(
            this.options
        )}`;
    }

    /**
     * Build the command
     * Returns Probel SW-P-08 - General Crosspoint Go Group Salvo Message CMD_121_0X79
     *
     * + This message is issued by the remote device to triggers the receiving device to set all routes in / clear the previously received CROSSPOINT CONNECT ON GO GROUP SALVO messages.
     * + A CROSSPOINT GO DONE GROUP SALVO ACKNOWLEDGE message (Command Byte 123) will be issued to indicate that the command has been executed.
     *
     * + Note : No individual CONNECTED messages (Command Byte 04) are issued.  It is the responsibility of the controlling / listening devices to use the CROSSPOINT CONNECT ON GO GROUP SALVO ACKNOWLEDGE (Command Byte 122) and
     * + CROSSPOINT GO DONE GROUP SALVO (Command Byte 123) to keep their tally Information up to date.
     *
     * + N.B. : The group salvo commands are only implemented on the XD and ECLIPSE router ranges.
     *
     * | Message | Command Byte | 121 - 0x79                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  | Bit[0]       | SavoFunction                                                                                                                       |
     * |         |              | 0 = Set previously received messages                                                                                               |
     * |         |              | 1 = Clear previously received messages                                                                                             |
     * | Byte 2  | Salvo number | Salvo group number                                                                                                                 |
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof CrossPointConnectMessageCommand
     */
    protected buildData(): Buffer {
        return new SmartBuffer({ size: 3 })
            .writeUInt8(this.id)
            .writeUInt8(this.options.salvoMessageFunction)
            .writeUInt8(this.params.salvoId)
            .toBuffer();
    }

    /**
     * Validates the command parameters, options and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {CrossPointGoGroupSalvoMessageCommandParams} params the command parameters
     * @param {CrossPointGoGroupSalvoMessageCommandOptions} options the command options
     * @memberof CrossPointGoGroupSalvoMessageCommand
     */
    private validateParams(params: CrossPointGoGroupSalvoMessageCommandParams): void {
        const validationErrors: Record<string, LocaleData> = {
            ...new CommandParamsValidator(params).validate()
        };

        if (Object.keys(validationErrors).length > 0) {
            const localeData = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(
                CommandErrorsKeys.CREATE_COMMAND_FAILURE
            );
            throw new ValidationError(localeData.description, validationErrors);
        }
    }
}
