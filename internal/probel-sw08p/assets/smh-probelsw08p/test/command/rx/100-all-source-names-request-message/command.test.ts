import { AllSourceNamesRequestMessageCommand } from '../../../../src/command/rx/100-all-source-names-request-message/command';
import { AllSourceNamesRequestMessageCommandParams } from '../../../../src/command/rx/100-all-source-names-request-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import { AllSourceNamesRequestMessageCommandOptions } from '../../../../src/command/rx/100-all-source-names-request-message/options';
import { LengthOfNamesRequired } from '../../../../src/command/shared/length-of-names-required';

describe('All Source Names Request Message Command', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params, options', () => {
            // Arrange
            const params: AllSourceNamesRequestMessageCommandParams = {
                matrixId: 0,
                levelId: 0
            };
            const options: AllSourceNamesRequestMessageCommandOptions = {
                lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
            };
            // Act
            const command = new AllSourceNamesRequestMessageCommand(params, options);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: AllSourceNamesRequestMessageCommandParams = {
                matrixId: -1,
                levelId: -1
            };

            const options: AllSourceNamesRequestMessageCommandOptions = {
                lengthOfNames: -1
            };
            // Act
            const fct = () => new AllSourceNamesRequestMessageCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: AllSourceNamesRequestMessageCommandParams = {
                matrixId: 256,
                levelId: 256
            };

            const options: AllSourceNamesRequestMessageCommandOptions = {
                lengthOfNames: 10
            };
            // Act
            const fct = () => new AllSourceNamesRequestMessageCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General All Source Names Request Message CMD_100_0X64', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.ALL_SOURCE_NAMES_REQUEST_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: AllSourceNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0
                };

                const options: AllSourceNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
                };
                // Act
                const command = new AllSourceNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '64 00 00', // data
                    3, // bytesCount
                    0x99, // checksum
                    '10 02 64 00 00 03 99 10 03' // buffer
                );
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: AllSourceNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0
                };

                const options: AllSourceNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.EIGHT_CHAR_NAMES
                };
                // Act
                const command = new AllSourceNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '64 00 01', // data
                    3, // bytesCount
                    0x98, // checksum
                    '10 02 64 00 01 03 98 10 03' // buffer
                );
            });
        });

        describe('Extended General General All Source Names Request Message CMD_228_0Xe4', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.EXTENDED.ALL_SOURCE_NAMES_REQUEST_MESSAGE;
            });
            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: AllSourceNamesRequestMessageCommandParams = {
                    matrixId: 16,
                    levelId: 0
                };

                const options: AllSourceNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
                };
                // Act
                const command = new AllSourceNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E4 10 00 00', // data
                    4, // bytesCount
                    0x08, // checksum
                    '10 02 E4 10 10 00 00 04 08 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: AllSourceNamesRequestMessageCommandParams = {
                    matrixId: 16,
                    levelId: 0
                };

                const options: AllSourceNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.EIGHT_CHAR_NAMES
                };
                // Act
                const command = new AllSourceNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E4 10 00 01', // data
                    4, // bytesCount
                    0x07, // checksum
                    '10 02 E4 10 10 00 01 04 07 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: AllSourceNamesRequestMessageCommandParams = {
                    matrixId: 16,
                    levelId: 0
                };

                const options: AllSourceNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.TWELVE_CHAR_NAMES
                };
                // Act
                const command = new AllSourceNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E4 10 00 02', // data
                    4, // bytesCount
                    0x06, // checksum
                    '10 02 E4 10 10 00 02 04 06 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: AllSourceNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    levelId: 16
                };

                const options: AllSourceNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
                };
                // Act
                const command = new AllSourceNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E4 00 10 00', // data
                    4, // bytesCount
                    0x08, // checksum
                    '10 02 E4 00 10 10 00 04 08 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: AllSourceNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    levelId: 16
                };

                const options: AllSourceNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.EIGHT_CHAR_NAMES
                };
                // Act
                const command = new AllSourceNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E4 00 10 01', // data
                    4, // bytesCount
                    0x07, // checksum
                    '10 02 E4 00 10 10 01 04 07 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: AllSourceNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    levelId: 16
                };

                const options: AllSourceNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.TWELVE_CHAR_NAMES
                };
                // Act
                const command = new AllSourceNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E4 00 10 02', // data
                    4, // bytesCount
                    0x06, // checksum
                    '10 02 E4 00 10 10 02 04 06 10 03' // buffer
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.EXTENDED.ALL_SOURCE_NAMES_REQUEST_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (matrixId=16 - levelId=16 - destinationId=16))', () => {
                // Arrange
                const params: AllSourceNamesRequestMessageCommandParams = {
                    matrixId: 16,
                    levelId: 16
                };

                const options: AllSourceNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.EIGHT_CHAR_NAMES
                };
                // Act
                const command = new AllSourceNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E4 10 10 01', // data
                    4, // bytesCount
                    0xf7, // checksum
                    '10 02 E4 10 10 10 10 01 04 F7 10 03' // buffer
                );
            });
        });
    });

    describe('to Log Description of the general and extended command', () => {
        it('Should log general command description', () => {
            // Arrange
            const params: AllSourceNamesRequestMessageCommandParams = {
                matrixId: 0,
                levelId: 0
            };

            const options: AllSourceNamesRequestMessageCommandOptions = {
                lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
            };
            // Act
            const command = new AllSourceNamesRequestMessageCommand(params, options);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });

        it('Should log extended command description', () => {
            // Arrange
            const params: AllSourceNamesRequestMessageCommandParams = {
                matrixId: 16,
                levelId: 16
            };

            const options: AllSourceNamesRequestMessageCommandOptions = {
                lengthOfNames: LengthOfNamesRequired.SIXTEEN_CHAR_NAMES
            };
            // Act
            const command = new AllSourceNamesRequestMessageCommand(params, options);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('extended')).toBe(true);
        });
    });
});
